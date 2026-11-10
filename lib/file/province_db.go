package file

import (
	"fmt"
	"path/filepath"

	"ehang.io/nps/lib/geo"
	"github.com/astaxie/beego/logs"
)

// ProvinceDbFileName 是「访问地区限制 → 仅允许指定省份」使用的 IP 库文件名。
//
// 库文件独立放在 conf/ 目录下，不随二进制打包；
// 服务启动时从该路径加载，后台「上传 IP 库」可随时更新。
const ProvinceDbFileName = "ip2region_v4.xdb"

// ProvinceDbPath 返回 IP 库在 conf 目录下的完整路径。
func (s *JsonDb) ProvinceDbPath() string {
	return filepath.Join(filepath.Dir(s.GlobalFilePath), ProvinceDbFileName)
}

// LoadProvinceDb 加载 conf 目录下的 IP 库。
//
// 库文件缺失或损坏时返回错误：此时省份模式不可用，
// 所有公网访问都会被判定为「未归属任何中国省份」而遭到拦截。
func (s *DbUtils) LoadProvinceDb() error {
	return geo.LoadProvinceIndex(s.JsonDb.ProvinceDbPath())
}

// SaveProvinceDb 校验并保存上传的 IP 库，然后重新加载使其立即生效。
//
// 先在内存里解析校验，确认是有效的 ip2region 库后才落盘；
// 落盘用「临时文件 + 改名」，避免写到一半被读到。
// 解析失败时不会动磁盘上的旧库。
func (s *DbUtils) SaveProvinceDb(data []byte) error {
	info, err := geo.ParseProvinceIndex(data)
	if err != nil {
		return err
	}
	path := s.JsonDb.ProvinceDbPath()
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("保存 IP 库失败: %w", err)
	}
	if err := geo.LoadProvinceIndex(path); err != nil {
		return err
	}
	logs.Info("IP 库已更新:", path, "收录省份数:", info.ProvinceCount, "IP 区间数:", info.RangeCount)
	return nil
}
