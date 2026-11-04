// Package geo 提供基于内置中国大陆 IP 段库的地域判定能力。
//
// 数据来源：https://github.com/gaoyifan/china-operator-ip （ip-lists/china.txt）
// 该库由 BGP 路由表与 IP WHOIS 信息合并生成，每行一条 CIDR，仅包含 IPv4。
//
// 判定规则：
//   - IPv4：命中中国 CIDR 段视为国内，否则视为国外。
//   - 私有 / 保留地址（10.x、172.16-31.x、192.168.x、127.x、169.254.x、
//     100.64-127.x、0.x、224.x 以上等）一律视为国内，直接放行。
//   - IPv6：内置库不含 IPv6，一律视为国内（放行），避免误杀。
package geo

import (
	_ "embed"
	"net"
	"strconv"
	"strings"
	"sync"
)

//go:embed china_ip_list.txt
var chinaIPList string

// cidrRange 表示一条已归一化的 IPv4 网段：[start, end] 闭区间。
// 转成 uint32 之后可以直接做区间比较，避免每次查询都做位运算。
type cidrRange struct {
	start uint32
	end   uint32
}

var (
	loadOnce sync.Once

	// chinaRanges 保存所有中国 CIDR 段，按 start 升序排列，用于二分查找。
	chinaRanges []cidrRange
)

// init 在包初始化时解析内置的中国 CIDR 段库。
// 解析失败时 chinaRanges 为空，此时 IsForeignIP 只会放行私有地址，
// 其余公网地址会被判定为国外（fail-closed），避免因数据缺失而全站失守。
func init() {
	loadOnce.Do(func() {
		chinaRanges = parseCIDRList(chinaIPList)
	})
}

// parseCIDRList 解析以 "\n" 分隔的 CIDR 文本，返回按 start 升序排列的网段列表。
// 非法条目会被跳过，不影响其余条目。
func parseCIDRList(content string) []cidrRange {
	lines := strings.Split(content, "\n")
	ranges := make([]cidrRange, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		slash := strings.IndexByte(line, '/')
		if slash < 0 {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(line[:slash]))
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			// 内置库只含 IPv4，IPv6 条目直接忽略。
			continue
		}
		bits, err := strconv.Atoi(strings.TrimSpace(line[slash+1:]))
		if err != nil || bits < 0 || bits > 32 {
			continue
		}
		start := ipv4ToUint32(ip4)
		mask := uint32(0)
		if bits > 0 {
			// 注意：不能写成 ^uint32(0) << n，编译器会按无类型常量求值并溢出。
			// 必须先把全 1 掩码放进变量，让移位在运行时按 uint32 语义执行。
			full := ^uint32(0)
			mask = full << (32 - bits)
		}
		start &= mask
		end := start | ^mask
		ranges = append(ranges, cidrRange{start: start, end: end})
	}
	sortRanges(ranges)
	return ranges
}

// sortRanges 使用插入排序对网段按 start 升序排列。
// 数据量在数千条级别，且只在启动时执行一次，插入排序足够且无额外依赖。
func sortRanges(ranges []cidrRange) {
	for i := 1; i < len(ranges); i++ {
		cur := ranges[i]
		j := i - 1
		for j >= 0 && ranges[j].start > cur.start {
			ranges[j+1] = ranges[j]
			j--
		}
		ranges[j+1] = cur
	}
}

// ipv4ToUint32 把 IPv4 地址转成大端 uint32，便于按数值大小比较。
// 注意：net.ParseIP 与 net.IPv4 返回的都是 16 字节表示，IPv4 字节位于 12-15；
// 这里必须先调用 To4() 归一化成 4 字节，否则会读到全零的前 4 字节。
func ipv4ToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

// IsPrivateIP 判断 IP 是否属于私有 / 保留 / 回环 / 链路本地等特殊地址段。
// 这类地址不可能是境外公网访问，一律放行。
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// 100.64.0.0/10：运营商级 NAT（CGNAT）共享地址段。
	if ip4 := ip.To4(); ip4 != nil {
		v := ipv4ToUint32(ip4)
		if v >= ipv4ToUint32(net.IPv4(100, 64, 0, 0)) &&
			v <= ipv4ToUint32(net.IPv4(100, 127, 255, 255)) {
			return true
		}
		// 240.0.0.0/4：保留网段（含组播 224.0.0.0/4）。
		if v >= ipv4ToUint32(net.IPv4(240, 0, 0, 0)) {
			return true
		}
	}
	return false
}

// IsChinaIP 判断 IP 是否属于中国大陆 IP 段。
// IPv6 与所有私有 / 保留地址都返回 true（视为国内，直接放行）。
func IsChinaIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if IsPrivateIP(ip) {
		return true
	}
	ip4 := ip.To4()
	if ip4 == nil {
		// 内置库只覆盖 IPv4，IPv6 视为国内放行。
		return true
	}
	return containsChinaRange(ipv4ToUint32(ip4))
}

// containsChinaRange 在已排序的网段列表中二分查找 ip 是否被覆盖。
func containsChinaRange(v uint32) bool {
	ranges := chinaRanges
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		switch {
		case v < ranges[mid].start:
			hi = mid - 1
		case v > ranges[mid].end:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// IsForeignIP 判断 IP 是否属于境外地址（不在中国大陆 IP 段内）。
// 私有 / 保留地址与 IPv6 返回 false，表示应当放行。
func IsForeignIP(ip net.IP) bool {
	return !IsChinaIP(ip)
}

// IsForeignIPStr 解析字符串形式的 IP 并判断是否境外。
// 解析失败时返回 false（放行），避免因异常输入误杀正常流量。
func IsForeignIPStr(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	return IsForeignIP(ip)
}

// RangeCount 返回已加载的中国 IP 段数量，便于诊断与测试。
func RangeCount() int {
	return len(chinaRanges)
}
