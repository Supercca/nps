// Package notify 提供外部通知能力，目前支持 Bark（iOS 推送）。
//
// 设计约束：
//   - 通知永远不能影响主链路。发送动作一律异步执行，失败只记日志，绝不 panic。
//   - 配置全部来自「全局参数」，可在 Web 后台热更新。
//   - 免打扰时段内的通知直接丢弃，不排队、不补发。
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

// QuietTimeFormat 是免打扰时段的配置格式。
const QuietTimeFormat = "15:04"

// barkClient 复用连接，避免每次推送都新建 TCP 连接。
var barkClient = &http.Client{Timeout: 10 * time.Second}

// barkPayload 是 Bark 推送的 JSON 请求体。
type barkPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Group 用于在手机端归类通知。
	Group string `json:"group,omitempty"`
	// Level: 1=active, 2=timeSensitive, 3=passive
	Level int `json:"level,omitempty"`
}

// inQuietPeriod 判断给定时间是否落在免打扰时段内。
// start/end 格式为 HH:MM；任一为空或格式非法时返回 false（不启用免打扰）。
// start 晚于 end 表示跨零点，例如 23:00-07:00。
func inQuietPeriod(now time.Time, start, end string) bool {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" || end == "" {
		return false
	}
	if _, err := time.Parse(QuietTimeFormat, start); err != nil {
		return false
	}
	if _, err := time.Parse(QuietTimeFormat, end); err != nil {
		return false
	}
	// HH:MM 是零填充定长格式，字典序与时间序一致，可以直接比较。
	cur := now.Format(QuietTimeFormat)
	if start <= end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}

// sendBark 向 Bark 服务器推送一条消息。
func sendBark(url, title, body string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("bark url is empty")
	}
	payload, err := json.Marshal(&barkPayload{
		Title: title,
		Body:  body,
		Group: "NPS",
		Level: 2,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := barkClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bark respond status %d", resp.StatusCode)
	}
	return nil
}

// SendBark 按全局配置推送一条 Bark 消息。
// 开关未打开、地址为空或当前处于免打扰时段时直接返回，不做任何网络请求。
func SendBark(title, body string) {
	settings := file.GetDb().GetGlobalSettings()
	if !settings.BarkEnabled {
		return
	}
	if strings.TrimSpace(settings.BarkUrl) == "" {
		logs.Warn("Bark 已开启但未配置推送地址，通知已跳过")
		return
	}
	if inQuietPeriod(time.Now(), settings.BarkQuietStart, settings.BarkQuietEnd) {
		logs.Info("当前处于 Bark 免打扰时段，通知已丢弃: %s", title)
		return
	}
	url := settings.BarkUrl
	// 异步发送：通知失败或超时都不应阻塞调用方（可能处于连接转发路径上）。
	go func() {
		if err := sendBark(url, title, body); err != nil {
			logs.Error("Bark 推送失败: %v", err)
			return
		}
		logs.Info("Bark 推送成功: %s", title)
	}()
}

// NotifyBlackIpAdded 通知「单个 IP 被加入全局黑名单」。
// source 说明该 IP 是从哪里来的，便于区分自动拦截与手动添加。
func NotifyBlackIpAdded(ip, source string) {
	body := fmt.Sprintf("IP: %s\n来源: %s\n时间: %s", ip, source, time.Now().Format("2006-01-02 15:04:05"))
	SendBark("NPS 黑名单新增 IP", body)
}

// NotifyBlackIpBatchAdded 通知「批量新增 IP 到全局黑名单」。
// 手动在 Web 后台一次粘贴大量 IP 时，逐条推送会刷屏，这里合并成一条。
func NotifyBlackIpBatchAdded(ips []string, source string) {
	if len(ips) == 0 {
		return
	}
	const maxShow = 20
	shown := ips
	suffix := ""
	if len(shown) > maxShow {
		shown = shown[:maxShow]
		suffix = fmt.Sprintf("\n... 共 %d 个，其余省略", len(ips))
	}
	body := fmt.Sprintf("新增 %d 个 IP\n来源: %s\n%s%s",
		len(ips), source, strings.Join(shown, ", "), suffix)
	SendBark("NPS 黑名单新增 IP", body)
}

// NotifyScheduledRestart 通知「服务端即将定时重启」。
// runAt 是计划的重启时刻，提醒在它之前若干分钟发出，
// 让运维人员有时间知道面板与隧道即将短暂中断。
func NotifyScheduledRestart(runAt time.Time) {
	body := fmt.Sprintf("服务端将在 %s 自动重启\n重启期间面板与所有隧道会短暂中断",
		runAt.Format("2006-01-02 15:04"))
	SendBark("NPS 服务端定时重启", body)
}

// NotifyLoginFail 通知「面板登录失败次数达到阈值」。
// ip 是来源地址，times 是统计窗口内的累计失败次数。
func NotifyLoginFail(ip string, times int) {
	body := fmt.Sprintf("来源 IP: %s\n失败次数: %d\n时间: %s",
		ip, times, time.Now().Format("2006-01-02 15:04:05"))
	SendBark("NPS 面板登录失败告警", body)
}

// NotifyLoginFromUnexpectedProvince 通知「面板登录来源的省份不在允许列表内」。
//
// 面板是直接对外暴露的，不经过隧道代理，因此不受「访问地区限制」的拦截，
// 这里只做告警：province 是来源 IP 所属省份，allowed 是当前允许的省份列表。
func NotifyLoginFromUnexpectedProvince(ip, province string, allowed []string) {
	allowedText := strings.Join(allowed, "、")
	if allowedText == "" {
		allowedText = "（未设置）"
	}
	body := fmt.Sprintf("来源 IP: %s\n所在省份: %s\n允许省份: %s\n时间: %s",
		ip, province, allowedText, time.Now().Format("2006-01-02 15:04:05"))
	SendBark("NPS 面板异地登录告警", body)
}
