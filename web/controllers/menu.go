package controllers

// MenuOption 描述左侧导航的一个菜单项。
// Key 是配置里使用的标识，Name 是 i18n 词条 key，Checked 表示当前是否被隐藏。
type MenuOption struct {
	Key     string
	Name    string
	Checked bool
}

// MenuOptions 是左侧导航的全部菜单项，顺序与页面显示顺序一致。
// 全局参数里的「隐藏菜单」就是按这些 key 勾选的。
var MenuOptions = []MenuOption{
	{Key: "index", Name: "word-dashboard"},
	{Key: "client", Name: "word-client"},
	{Key: "host", Name: "scheme-host"},
	{Key: "tcp", Name: "scheme-tcp"},
	{Key: "udp", Name: "scheme-udp"},
	{Key: "http", Name: "scheme-httpproxy"},
	{Key: "socks5", Name: "scheme-socks5"},
	{Key: "secret", Name: "scheme-secret"},
	{Key: "p2p", Name: "scheme-p2p"},
	{Key: "file", Name: "scheme-file"},
	{Key: "global", Name: "word-globalparam"},
	{Key: "log", Name: "word-serverlog"},
	{Key: "help", Name: "word-help"},
}

// MenuKeys 是全部合法菜单 key 的集合，用于过滤用户提交的非法值。
var MenuKeys = func() map[string]bool {
	keys := make(map[string]bool, len(MenuOptions))
	for _, item := range MenuOptions {
		keys[item.Key] = true
	}
	return keys
}()

// ProtectedMenus 是不能被隐藏的菜单项。
// 「全局参数」是唯一能修改本配置的地方，一旦隐藏就无法再恢复。
var ProtectedMenus = map[string]bool{
	"global": true,
}

// hiddenMenuMap 把隐藏列表转成 map，方便模板里直接按 key 取值。
func hiddenMenuMap(hidden []string) map[string]bool {
	m := make(map[string]bool, len(hidden))
	for _, key := range hidden {
		m[key] = true
	}
	return m
}

// menuOptionsWithState 返回带勾选状态的菜单项列表，供全局参数页渲染。
func menuOptionsWithState(hidden []string) []MenuOption {
	state := hiddenMenuMap(hidden)
	options := make([]MenuOption, 0, len(MenuOptions))
	for _, item := range MenuOptions {
		options = append(options, MenuOption{
			Key:     item.Key,
			Name:    item.Name,
			Checked: state[item.Key],
		})
	}
	return options
}
