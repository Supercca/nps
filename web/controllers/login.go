package controllers

import (
	"fmt"
	"github.com/astaxie/beego/cache"
	"github.com/astaxie/beego/utils/captcha"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/geo"
	"ehang.io/nps/lib/notify"
	"ehang.io/nps/lib/version"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

type LoginController struct {
	beego.Controller
}

// 面板登录失败的统计参数。
const (
	// loginFailWindow 是登录失败计数的统计窗口：
	// 超过这个时间没有新的失败，计数就重新开始。
	loginFailWindow = 60 * time.Second
	// loginFailLockoutTimes 是触发临时锁定的失败次数。
	loginFailLockoutTimes = 10
)

var ipRecord sync.Map
var cpt *captcha.Captcha

// loginAlertLog 是登录告警专用的日志限流器。
//
// 单独一个实例，不与代理层的拦截日志共用预算：
// 被暴力破解时来源 IP 可能很多，需要独立限流，
// 也不能让代理侧的日志洪水把登录告警挤掉。
var loginAlertLog = common.NewBlockLogLimiter(time.Minute, 10)

// loginFailRecord 记录某个来源 IP 的登录失败情况。
//
// 计数与时间戳都用互斥锁保护：多个并发登录请求会同时命中同一个 IP，
// 裸字段累加会互相覆盖，导致锁定阈值形同虚设。
type loginFailRecord struct {
	mu        sync.Mutex
	failTimes int
	lastTime  time.Time
	// alerted 表示本轮窗口内是否已经告警过，
	// 避免同一个 IP 反复触发推送。
	alerted bool
}

// expire 在距上次失败超过 window 后清零计数（并解除告警标记）。
func (r *loginFailRecord) expire(now time.Time, window time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastTime.IsZero() && now.Sub(r.lastTime) >= window {
		r.failTimes = 0
		r.alerted = false
	}
}

// locked 判断该 IP 是否已被临时锁定。
func (r *loginFailRecord) locked(limit int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failTimes >= limit
}

// fail 记一次失败，返回累计失败次数，以及本轮窗口内是否首次达到告警阈值。
// threshold <= 0 表示不告警。
func (r *loginFailRecord) fail(now time.Time, threshold int) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failTimes++
	r.lastTime = now
	shouldAlert := threshold > 0 && !r.alerted && r.failTimes >= threshold
	if shouldAlert {
		r.alerted = true
	}
	return r.failTimes, shouldAlert
}

// lastFailureTime 返回最后一次失败的时间。
func (r *loginFailRecord) lastFailureTime() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastTime
}

// InitCaptcha registers the captcha image route and the create_captcha template
// function. The image URL prefix must include web_base_url, otherwise the login
// page would request the captcha from the site root and fail behind a reverse
// proxy that only forwards the web_base_url sub path.
//
// It must be called after beego.LoadAppConfig, so it cannot live in init():
// package init() runs before main() loads conf/nps.conf, where web_base_url
// would still be empty.
func InitCaptcha() {
	// use beego cache system store the captcha data
	store := cache.NewMemoryCache()
	base := strings.TrimSuffix(beego.AppConfig.String("web_base_url"), "/")
	if base != "" && !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	cpt = captcha.NewWithFilter(base+"/captcha/", store)
}

func (self *LoginController) Index() {
	// Try login implicitly, will succeed if it's configured as no-auth(empty username&password).
	webBaseUrl := beego.AppConfig.String("web_base_url")
	if self.doLogin("", "", false) {
		self.Redirect(webBaseUrl+"/index/index", 302)
	}
	self.Data["web_base_url"] = webBaseUrl
	globalSettings := file.GetDb().GetGlobalSettings()
	self.Data["register_allow"] = globalSettings.AllowUserRegister
	self.Data["captcha_open"] = globalSettings.OpenCaptcha
	self.Data["version"] = version.VERSION
	self.TplName = "login/index.html"
}

func (self *LoginController) Verify() {
	username := self.GetString("username")
	password := self.GetString("password")
	captchaOpen := file.GetDb().GetGlobalSettings().OpenCaptcha
	if captchaOpen {
		if !cpt.VerifyReq(self.Ctx.Request) {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "the verification code is wrong, please get it again and try again"}
			self.ServeJSON()
		}
	}
	if self.doLogin(username, password, true) {
		self.Data["json"] = map[string]interface{}{"status": 1, "msg": "login success"}
	} else {
		self.Data["json"] = map[string]interface{}{"status": 0, "msg": "username or password incorrect"}
	}
	self.ServeJSON()
}

func (self *LoginController) doLogin(username, password string, explicit bool) bool {
	clearIprecord()
	ip, _, _ := net.SplitHostPort(self.Ctx.Request.RemoteAddr)
	if v, ok := ipRecord.Load(ip); ok {
		vv := v.(*loginFailRecord)
		vv.expire(time.Now(), loginFailWindow)
		if vv.locked(loginFailLockoutTimes) {
			return false
		}
	}
	var auth bool
	if password == beego.AppConfig.String("web_password") && username == beego.AppConfig.String("web_username") {
		self.SetSession("isAdmin", true)
		self.DelSession("clientId")
		self.DelSession("username")
		auth = true
		server.Bridge.Register.Store(common.GetIpByAddr(self.Ctx.Input.IP()), time.Now().Add(time.Hour*time.Duration(2)))
	}
	allowUserLogin := file.GetDb().GetGlobalSettings().AllowUserLogin
	if allowUserLogin && !auth {
		file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
			v := value.(*file.Client)
			if !v.Status || v.NoDisplay {
				return true
			}
			if v.WebUserName == "" && v.WebPassword == "" {
				if username != "user" || v.VerifyKey != password {
					return true
				} else {
					auth = true
				}
			}
			if !auth && v.WebPassword == password && v.WebUserName == username {
				auth = true
			}
			if auth {
				self.SetSession("isAdmin", false)
				self.SetSession("clientId", v.Id)
				self.SetSession("username", v.WebUserName)
				return false
			}
			return true
		})
	}
	if auth {
		self.SetSession("auth", true)
		ipRecord.Delete(ip)
		// 登录成功，但来源省份不在允许列表内 —— 面板是直接对外暴露的，
		// 不经过隧道代理，因此不受「访问地区限制」拦截，这里只做告警。
		notifyLoginFromUnexpectedProvince(ip)
		return true

	}
	// 登录失败计数：只有显式提交了密码的失败才计入。
	if explicit {
		threshold := file.GetDb().GetGlobalSettings().LoginFailAlertThreshold
		v, _ := ipRecord.LoadOrStore(ip, &loginFailRecord{})
		vv := v.(*loginFailRecord)
		times, shouldAlert := vv.fail(time.Now(), threshold)
		if shouldAlert {
			// 走限流器：被暴力破解时可能有大量来源 IP 同时触发告警，
			// 不能把推送服务打爆；只有真正写出的那一条才推送通知。
			if loginAlertLog.Log("loginfail:"+ip,
				fmt.Sprintf("面板登录失败次数达到阈值: IP[%s] 累计失败 %d 次", ip, times)) {
				notify.NotifyLoginFail(ip, times)
			}
		}
	}
	return false
}

// notifyLoginFromUnexpectedProvince 在「仅允许指定省份访问」模式下，
// 检查登录来源 IP 所属省份是否在允许列表内，不在则推送一条告警。
//
// 查不到省份归属的情况（内网地址、IPv6、境外地址）不做判定，也不告警。
func notifyLoginFromUnexpectedProvince(ip string) {
	limit := file.GetDb().GetRegionLimit()
	if limit.Mode != file.RegionLimitModeAllowProvinces {
		return
	}
	netIP := net.ParseIP(ip)
	if netIP == nil {
		return
	}
	province := geo.LookupProvince(netIP)
	if !shouldAlertLoginProvince(limit.Mode, limit.Allowed, province) {
		return
	}
	allowed := file.GetDb().GetGlobalSettings().AllowedProvinces
	// 与失败告警共用同一个限流器，但用不同的 key 前缀，互不抢占冷却额度。
	if loginAlertLog.Log("loginprovince:"+ip,
		fmt.Sprintf("面板登录来源省份不在允许列表内: IP[%s] 省份[%s]", ip, province)) {
		notify.NotifyLoginFromUnexpectedProvince(ip, province, allowed)
	}
}

// shouldAlertLoginProvince 判断是否需要为「登录来源省份不在允许列表内」告警。
//
// mode 是访问地区限制模式，allowed 是允许省份集合，province 是来源 IP 所属省份
// （查不到归属时为空字符串）。
func shouldAlertLoginProvince(mode int, allowed map[string]bool, province string) bool {
	// 只有「仅允许指定省份访问」模式才需要判定。
	if mode != file.RegionLimitModeAllowProvinces {
		return false
	}
	// 查不到省份归属（内网、IPv6、境外）时不判定。
	if province == "" {
		return false
	}
	return !allowed[province]
}
func (self *LoginController) Register() {
	if self.Ctx.Request.Method == "GET" {
		self.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
		self.Data["version"] = version.VERSION
		self.TplName = "login/register.html"
	} else {
		if !file.GetDb().GetGlobalSettings().AllowUserRegister {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "register is not allow"}
			self.ServeJSON()
			return
		}
		if self.GetString("username") == "" || self.GetString("password") == "" || self.GetString("username") == beego.AppConfig.String("web_username") {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "please check your input"}
			self.ServeJSON()
			return
		}
		t := &file.Client{
			Status:      true,
			Cnf:         &file.Config{},
			WebUserName: self.GetString("username"),
			WebPassword: self.GetString("password"),
			Flow:        &file.Flow{},
		}
		if err := file.GetDb().NewClient(t); err != nil {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": err.Error()}
		} else {
			self.Data["json"] = map[string]interface{}{"status": 1, "msg": "register success"}
		}
		self.ServeJSON()
	}
}

func (self *LoginController) Out() {
	self.SetSession("auth", false)
	self.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
}

func clearIprecord() {
	rand.Seed(time.Now().UnixNano())
	x := rand.Intn(100)
	if x == 1 {
		ipRecord.Range(func(key, value interface{}) bool {
			v := value.(*loginFailRecord)
			if time.Now().Unix()-v.lastFailureTime().Unix() >= 60 {
				ipRecord.Delete(key)
			}
			return true
		})
	}
}
