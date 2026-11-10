package geo

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ip2region 离线库（xdb v4，仅覆盖 IPv4）。
//
// 内置一份在二进制里，保证开箱即用；同时允许在 conf/ 目录下放一份更新的库覆盖内置版本，
// 这样更新 IP 库不需要重新编译服务端。
//
// 数据来源：https://github.com/lionsoul2014/ip2region
// 代码 Apache-2.0；数据由社区维护，不保证及时更新。
//
//go:embed ip2region_v4.xdb
var embeddedXDB []byte

const (
	// xdbHeaderLength 是 xdb 文件头长度（字节）。
	xdbHeaderLength = 256
	// xdbVectorRows / xdbVectorCols 是向量索引的行列数。
	xdbVectorRows = 256
	xdbVectorCols = 256
	// xdbVectorSize 是向量索引中每个单元的字节数（起始指针 + 结束指针）。
	xdbVectorSize = 8
	// xdbSegmentSize 是段索引项长度：起始 IP(4) + 结束 IP(4) + 数据长度(2) + 数据偏移(4)。
	xdbSegmentSize = 14
	// xdbIPVersion4 表示该库只包含 IPv4 数据。
	xdbIPVersion4 = 4
	// xdbRuntimePtrBytes4 表示偏移量用 4 字节存储。
	xdbRuntimePtrBytes4 = 4
	// xdbVersion3 是本实现支持的 xdb 版本号。
	xdbVersion3 = 3
)

// XdbFileName 是 IP 库的默认文件名。
const XdbFileName = "ip2region_v4.xdb"

// provinceRange 是一段连续归属同一省份的 IPv4 区间：[start, end] 闭区间。
type provinceRange struct {
	start uint32
	end   uint32
	// provIdx 是省份名在 provinceNames 中的下标。
	provIdx uint16
}

var (
	provinceOnce sync.Once
	// provinceRanges 保存合并后的省份区间，按 start 升序排列，可直接二分查找。
	provinceRanges []provinceRange
	// provinceNames 保存规范省份名，下标与 provinceRanges.provIdx 对应。
	provinceNames []string
	// provinceErr 记录 IP 库加载或解析失败的原因。
	provinceErr error
)

// loadProvinceIndex 惰性加载省份索引。
// 只有真正用到省份限制时才会解析 10MB 级别的 IP 库，避免拖慢服务启动。
func loadProvinceIndex() {
	provinceOnce.Do(func() {
		data, err := loadXDBData()
		if err != nil {
			provinceErr = err
			return
		}
		ranges, names, err := parseProvinceIndex(data)
		if err != nil {
			provinceErr = err
			return
		}
		provinceRanges = ranges
		provinceNames = names
	})
}

// loadXDBData 读取 IP 库内容。
// 优先使用运行目录 conf/ 下的同名文件（便于用户自行更新 IP 库），否则回退到内置版本。
func loadXDBData() ([]byte, error) {
	path := filepath.Join("conf", XdbFileName)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("geo: 读取 %s 失败: %w", path, err)
		}
		return data, nil
	}
	if len(embeddedXDB) == 0 {
		return nil, errors.New("geo: 内置 IP 库为空")
	}
	return embeddedXDB, nil
}

// parseProvinceIndex 解析 xdb 数据，返回合并后的省份区间与省份名表。
func parseProvinceIndex(data []byte) ([]provinceRange, []string, error) {
	if len(data) < xdbHeaderLength+xdbVectorRows*xdbVectorCols*xdbVectorSize {
		return nil, nil, errors.New("geo: IP 库文件过小，格式不正确")
	}
	version := binary.LittleEndian.Uint16(data[0:])
	if version != xdbVersion3 {
		return nil, nil, fmt.Errorf("geo: 不支持的 IP 库版本 %d", version)
	}
	ipVersion := binary.LittleEndian.Uint16(data[16:])
	if ipVersion != xdbIPVersion4 {
		return nil, nil, fmt.Errorf("geo: 仅支持 IPv4 的 IP 库，当前为 %d", ipVersion)
	}
	ptrBytes := binary.LittleEndian.Uint16(data[18:])
	if ptrBytes != xdbRuntimePtrBytes4 {
		return nil, nil, fmt.Errorf("geo: 不支持的指针长度 %d", ptrBytes)
	}

	startPtr := binary.LittleEndian.Uint32(data[8:])
	endPtr := binary.LittleEndian.Uint32(data[12:])
	if startPtr == 0 || endPtr <= startPtr {
		return nil, nil, errors.New("geo: IP 库段索引区间无效")
	}
	count := int((endPtr - startPtr) / xdbSegmentSize)
	if count <= 0 {
		return nil, nil, errors.New("geo: IP 库段索引为空")
	}

	nameIndex := make(map[string]uint16, 40)
	names := make([]string, 0, 40)
	raw := make([]provinceRange, 0, count)

	for i := 0; i < count; i++ {
		offset := startPtr + uint32(i)*xdbSegmentSize
		if int(offset)+xdbSegmentSize > len(data) {
			break
		}
		item := data[offset : offset+xdbSegmentSize]
		// v4 的段索引把 IP 按 uint32 小端存放，这里读出来就是可比较的数值。
		start := binary.LittleEndian.Uint32(item[0:4])
		end := binary.LittleEndian.Uint32(item[4:8])
		dataLen := int(binary.LittleEndian.Uint16(item[8:10]))
		dataPtr := binary.LittleEndian.Uint32(item[10:14])
		if dataLen == 0 {
			continue
		}
		if int(dataPtr)+dataLen > len(data) {
			continue
		}
		region := string(data[dataPtr : dataPtr+uint32(dataLen)])
		name := provinceOfRegion(region)
		if name == "" {
			continue
		}
		idx, ok := nameIndex[name]
		if !ok {
			idx = uint16(len(names))
			nameIndex[name] = idx
			names = append(names, name)
		}
		raw = append(raw, provinceRange{start: start, end: end, provIdx: idx})
	}

	if len(raw) == 0 {
		return nil, nil, errors.New("geo: IP 库中没有中国省份数据")
	}

	// 段索引本身有序，但保险起见先排序再合并，保证二分查找的前提成立。
	sort.Slice(raw, func(i, j int) bool { return raw[i].start < raw[j].start })

	merged := raw[:0]
	for _, item := range raw {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			// 相邻（区间连续）或重叠且同属一个省份时可以合并成一条。
			if last.provIdx == item.provIdx &&
				(item.start <= last.end || (last.end < 0xFFFFFFFF && item.start == last.end+1)) {
				if item.end > last.end {
					last.end = item.end
				}
				continue
			}
		}
		merged = append(merged, item)
	}
	return merged, names, nil
}

// provinceOfRegion 从 xdb 的归属串里取出规范省份名。
//
// 归属串格式为「国家|省份|城市|运营商|国家代码」，例如 "中国|广东省|深圳市|电信|CN"。
// 只有国家为中国时才认为是中国省份；取不到省份名的（例如 "中国|0|0|0|CN"）返回空串。
func provinceOfRegion(region string) string {
	parts := strings.Split(region, "|")
	if len(parts) < 2 {
		return ""
	}
	country := strings.TrimSpace(parts[0])
	if country != "中国" && country != "中华人民共和国" {
		return ""
	}
	return NormalizeProvinceName(parts[1])
}

// LookupProvince 返回 IP 所属的中国省级行政区规范简称，例如 "广东"。
//
// 返回空字符串表示该 IP 不属于任何中国省级行政区（境外地址或未被 IP 库收录）。
// 只有 IPv4 会参与判定：内置 IP 库不覆盖 IPv6，传入 IPv6 一律返回空串。
func LookupProvince(ip net.IP) string {
	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}
	loadProvinceIndex()
	if len(provinceRanges) == 0 {
		return ""
	}
	v := ipv4ToUint32(ip4)
	lo, hi := 0, len(provinceRanges)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		switch {
		case v < provinceRanges[mid].start:
			hi = mid - 1
		case v > provinceRanges[mid].end:
			lo = mid + 1
		default:
			return provinceNames[provinceRanges[mid].provIdx]
		}
	}
	return ""
}

// ProvinceDBReady 返回 IP 库是否可用，以及不可用时的原因。
// 供后台页面判断「按省份限制访问」是否可以选择。
func ProvinceDBReady() (bool, string) {
	loadProvinceIndex()
	if provinceErr != nil {
		return false, provinceErr.Error()
	}
	return true, ""
}

// ProvinceRangeCount 返回合并后的省份区间数量，便于诊断与测试。
func ProvinceRangeCount() int {
	loadProvinceIndex()
	return len(provinceRanges)
}
