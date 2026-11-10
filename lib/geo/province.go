package geo

import "strings"

// 大区（地理分区）标识。用于把 34 个省级行政区分组展示。
const (
	RegionNorth     = "north"
	RegionNortheast = "northeast"
	RegionEast      = "east"
	RegionCentral   = "central"
	RegionSouth     = "south"
	RegionSouthwest = "southwest"
	RegionNorthwest = "northwest"
)

// ProvinceInfo 描述一个可用于「按省份限制访问」的省级行政区。
type ProvinceInfo struct {
	// Name 是规范化后的中文简称，例如 "广东"。
	// 它同时也是 global.json 中保存的省份标识，配置与代码都以它为准。
	Name string
	// LangKey 是前端 i18n 词条 key，用于显示中英文名称。
	LangKey string
	// Group 是所属大区标识，取值为本文件的 RegionXxx 常量。
	Group string
	// GroupLangKey 是大区的 i18n 词条 key。
	GroupLangKey string
}

// ProvinceGroup 是按大区分组后的省级行政区列表。
type ProvinceGroup struct {
	// Name 是大区标识。
	Name string
	// LangKey 是大区的 i18n 词条 key。
	LangKey string
	// Provinces 是该大区下的省级行政区。
	Provinces []ProvinceInfo
}

// provinceDef 是省级行政区的静态定义。
type provinceDef struct {
	name    string
	langKey string
	group   string
}

// provinceDefs 是全部 34 个省级行政区的定义，顺序即前端展示顺序。
//
// 省份名称一律使用「中文简称」（如 广东、内蒙古、香港），
// 既便于阅读，也和 ip2region 返回的原始名称做了归一化映射。
var provinceDefs = []provinceDef{
	{"北京", "word-province-beijing", RegionNorth},
	{"天津", "word-province-tianjin", RegionNorth},
	{"河北", "word-province-hebei", RegionNorth},
	{"山西", "word-province-shanxi", RegionNorth},
	{"内蒙古", "word-province-neimenggu", RegionNorth},

	{"辽宁", "word-province-liaoning", RegionNortheast},
	{"吉林", "word-province-jilin", RegionNortheast},
	{"黑龙江", "word-province-heilongjiang", RegionNortheast},

	{"上海", "word-province-shanghai", RegionEast},
	{"江苏", "word-province-jiangsu", RegionEast},
	{"浙江", "word-province-zhejiang", RegionEast},
	{"安徽", "word-province-anhui", RegionEast},
	{"福建", "word-province-fujian", RegionEast},
	{"江西", "word-province-jiangxi", RegionEast},
	{"山东", "word-province-shandong", RegionEast},
	{"台湾", "word-province-taiwan", RegionEast},

	{"河南", "word-province-henan", RegionCentral},
	{"湖北", "word-province-hubei", RegionCentral},
	{"湖南", "word-province-hunan", RegionCentral},

	{"广东", "word-province-guangdong", RegionSouth},
	{"广西", "word-province-guangxi", RegionSouth},
	{"海南", "word-province-hainan", RegionSouth},
	{"香港", "word-province-hongkong", RegionSouth},
	{"澳门", "word-province-macau", RegionSouth},

	{"重庆", "word-province-chongqing", RegionSouthwest},
	{"四川", "word-province-sichuan", RegionSouthwest},
	{"贵州", "word-province-guizhou", RegionSouthwest},
	{"云南", "word-province-yunnan", RegionSouthwest},
	{"西藏", "word-province-xizang", RegionSouthwest},

	{"陕西", "word-province-shaanxi", RegionNorthwest},
	{"甘肃", "word-province-gansu", RegionNorthwest},
	{"青海", "word-province-qinghai", RegionNorthwest},
	{"宁夏", "word-province-ningxia", RegionNorthwest},
	{"新疆", "word-province-xinjiang", RegionNorthwest},
}

// regionOrder 是大区的展示顺序。
var regionOrder = []struct {
	name    string
	langKey string
}{
	{RegionNorth, "word-region-north"},
	{RegionNortheast, "word-region-northeast"},
	{RegionEast, "word-region-east"},
	{RegionCentral, "word-region-central"},
	{RegionSouth, "word-region-south"},
	{RegionSouthwest, "word-region-southwest"},
	{RegionNorthwest, "word-region-northwest"},
}

// provinceSet 保存全部规范省份名，用于快速查表。
var provinceSet = func() map[string]bool {
	set := make(map[string]bool, len(provinceDefs))
	for _, def := range provinceDefs {
		set[def.name] = true
	}
	return set
}()

// provinceAliases 保存官方全称到规范简称的映射。
// ip2region 库里的名称大多数是「广东省」「北京市」这类带后缀的形式，
// 而自治区与特别行政区则是「内蒙古」「香港特别行政区」，统一在这里收敛。
var provinceAliases = map[string]string{
	"内蒙古自治区":   "内蒙古",
	"广西壮族自治区":  "广西",
	"西藏自治区":    "西藏",
	"宁夏回族自治区":  "宁夏",
	"新疆维吾尔自治区": "新疆",
	"香港特别行政区":  "香港",
	"澳门特别行政区":  "澳门",
}

// provinceSuffixes 是归一化时依次尝试剥离的行政区后缀。
// 长的排在前面，避免「广西壮族自治区」被先按「自治区」截成「广西壮族」。
var provinceSuffixes = []string{
	"特别行政区",
	"维吾尔自治区",
	"壮族自治区",
	"回族自治区",
	"自治区",
	"省",
	"市",
}

// NormalizeProvinceName 把各种写法的省份名归一化成规范简称。
//
// 输入可以是 "广东省"、"广西壮族自治区"、"香港特别行政区"、"广东" 等形式；
// 无法识别时返回空字符串。
func NormalizeProvinceName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if provinceSet[name] {
		return name
	}
	if alias, ok := provinceAliases[name]; ok {
		return alias
	}
	for _, suffix := range provinceSuffixes {
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		trimmed := strings.TrimSuffix(name, suffix)
		if provinceSet[trimmed] {
			return trimmed
		}
		if alias, ok := provinceAliases[trimmed]; ok {
			return alias
		}
	}
	return ""
}

// Provinces 返回全部省级行政区定义，按大区分组顺序排列。
func Provinces() []ProvinceInfo {
	result := make([]ProvinceInfo, 0, len(provinceDefs))
	for _, def := range provinceDefs {
		result = append(result, ProvinceInfo{
			Name:         def.name,
			LangKey:      def.langKey,
			Group:        def.group,
			GroupLangKey: groupLangKey(def.group),
		})
	}
	return result
}

// ProvincesByGroup 返回按大区分组的省级行政区列表，供前端渲染勾选框。
func ProvincesByGroup() []ProvinceGroup {
	groups := make([]ProvinceGroup, 0, len(regionOrder))
	for _, region := range regionOrder {
		group := ProvinceGroup{Name: region.name, LangKey: region.langKey}
		for _, def := range provinceDefs {
			if def.group == region.name {
				group.Provinces = append(group.Provinces, ProvinceInfo{
					Name:         def.name,
					LangKey:      def.langKey,
					Group:        def.group,
					GroupLangKey: region.langKey,
				})
			}
		}
		groups = append(groups, group)
	}
	return groups
}

// groupLangKey 返回大区的 i18n 词条 key。
func groupLangKey(group string) string {
	for _, region := range regionOrder {
		if region.name == group {
			return region.langKey
		}
	}
	return ""
}
