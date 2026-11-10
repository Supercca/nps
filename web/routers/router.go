package routers

import (
	"ehang.io/nps/web/controllers"
	"github.com/astaxie/beego"
)

func Init() {
	// The captcha image path and its filter are built from web_base_url, so it
	// has to be registered here (after the config is loaded) instead of in an
	// init() function.
	controllers.InitCaptcha()

	// IP 库上传使用显式路由，排除自动路由生成的 /global/provinceupload 别名。
	beego.ExceptMethodAppend("ProvinceUpload")

	web_base_url := beego.AppConfig.String("web_base_url")
	if len(web_base_url) > 0 {
		ns := beego.NewNamespace(web_base_url,
			beego.NSRouter("/", &controllers.IndexController{}, "*:Index"),
			beego.NSAutoRouter(&controllers.IndexController{}),
			beego.NSAutoRouter(&controllers.LoginController{}),
			beego.NSAutoRouter(&controllers.ClientController{}),
			beego.NSAutoRouter(&controllers.AuthController{}),
			beego.NSRouter("/auth/ipwhiteauth", &controllers.AuthController{}, "*:IpWhiteAuth"),
			beego.NSRouter("/global/province/upload", &controllers.GlobalController{}, "POST:ProvinceUpload"),
			beego.NSAutoRouter(&controllers.GlobalController{}),
			beego.NSAutoRouter(&controllers.LogController{}),
		)
		beego.AddNamespace(ns)
	} else {
		beego.Router("/", &controllers.IndexController{}, "*:Index")
		beego.AutoRouter(&controllers.IndexController{})
		beego.AutoRouter(&controllers.LoginController{})
		beego.AutoRouter(&controllers.ClientController{})
		beego.AutoRouter(&controllers.AuthController{})
		beego.Router("/auth/ipwhiteauth", &controllers.AuthController{}, "*:IpWhiteAuth")
		beego.Router("/global/province/upload", &controllers.GlobalController{}, "POST:ProvinceUpload")
		beego.AutoRouter(&controllers.GlobalController{})
		beego.AutoRouter(&controllers.LogController{})

	}
}
