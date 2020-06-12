package main

import (
	"GoMonitor/logdoo"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/fsnotify.v1"
)

const (
	WatcherModify = 1
	WatcherStop   = 2
)

const (
	IsDebug   = 1
	IsRelease = 2
)

var monitorCfg = NewMonitorCfg()
var monitorService = NewMonitorService()
var monitorEmail = NewEmail()

func main() {
	RunWindowService(IsDebug)
}

func ServerMain() {
	//初始化日记
	if logPath, err := CreateLogDir("monitorServiceLog"); err == nil {
		logdoo.SetDayLogHandleDir(logPath)
		logdoo.SetDayLogHandleFileSize(800)
		defer logdoo.Close()
	} else {
		panic("CreateLogDir err")
	}

	if monitorCfg == nil {
		logdoo.ErrorDoo("init monitor config file fail")
		return
	}

	logdoo.InfoDoo("***service start***")

	//获取配置文件目录
	cfgPath, err := GetCfgPath()
	if err != nil {
		logdoo.ErrorDoo("GetCfgPath err", err)
		return
	}

	if err := LoadCfgService(monitorCfg, monitorService, monitorEmail, cfgPath); err != nil {
		logdoo.ErrorDoo(err)
	}
	monitorService.StartMonitor(monitorCfg, monitorEmail)

	hasModify := make(chan int)
	defer close(hasModify)
	timer := time.NewTicker(time.Duration(monitorCfg.GetRefreshTime()) * time.Second) //默认是5分钟刷新一次
	go WatchCfgFile(cfgPath, hasModify)

	for {
		select {
		case event := <-hasModify:
			if event == WatcherModify {
				if err := UpdateCfgService(monitorCfg, monitorService, monitorEmail, cfgPath); err != nil {
					logdoo.ErrorDoo(err)
				}
			}

		case <-timer.C:
			if err := UpdateMoniService(monitorCfg, monitorService, monitorEmail, cfgPath); err != nil {
				logdoo.ErrorDoo(err)
			}

		case <-monitorService.stopChan:
			monitorService.Release()
			break
		}
	}
}

func CloseService() {
	monitorService.stopChan <- true
	logdoo.InfoDoo("***service close***")
}

//LoadCfgService 根据配置文件加载监控服务信息
func LoadCfgService(mc *MonitorCfg, ms *MonitorService, e *Email, cfgPath string) error {
	if err := mc.LoadCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadCfg err:%s", err)
	}

	e.UpdateEmail(mc.GetEmailData())
	specServices := mc.GetSpecServices()
	partServices := mc.GetPartServices()
	ms.AddSpecService(specServices)
	ms.AddPartService(partServices)
	services := ms.GetMointorServices()
	var str string
	for _, service := range services {
		str += service + "\n"
	}
	logdoo.InfoDoo(fmt.Sprintf("LoadCfgService cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//UpdateCfgService 配置文件修改时更新同时更新监控数据
func UpdateCfgService(mc *MonitorCfg, ms *MonitorService, e *Email, cfgPath string) error {
	if err := mc.LoadCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadCfg err:%s", err)
	}

	e.UpdateEmail(mc.GetEmailData())
	specServices := mc.GetSpecServices()
	partServices := mc.GetPartServices()
	ms.UpdateServices(specServices, partServices)
	services := ms.GetMointorServices()
	var str string
	for _, service := range services {
		str += service + "\n"
	}
	logdoo.InfoDoo(fmt.Sprintf("UpdateCfgService cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//UpdateMoniService 用于定时任务定时刷新任务管理器中需要监控的服务
func UpdateMoniService(mc *MonitorCfg, ms *MonitorService, e *Email, cfgPath string) error {

	specServices := mc.GetSpecServices()
	partServices := mc.GetPartServices()
	ms.UpdateServices(specServices, partServices)
	services := ms.GetMointorServices()
	var str string
	for _, service := range services {
		str += service + "\n"
	}
	logdoo.InfoDoo(fmt.Sprintf("UpdateMoniService cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//WatchCfgFile 监控配置文件是否有被修改了
func WatchCfgFile(cfgPath string, hasModify chan<- int) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logdoo.WarnDoo("watcher cfg file modify err:", err)
	}
	defer watcher.Close()

	stop := make(chan int)
	defer close(stop)

	//开协程监听文件是否别修改
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					hasModify <- WatcherStop
					stop <- WatcherStop
					return
				}

				//当前监控的配置文件有write操作了证明被修改了
				if event.Op&fsnotify.Write == fsnotify.Write {
					hasModify <- WatcherModify
				}

			case _, ok := <-watcher.Errors:
				if !ok {
					hasModify <- WatcherStop
					stop <- WatcherStop
					return
				}
			}
		}
	}()

	if err := watcher.Add(cfgPath); err != nil {
		logdoo.WarnDoo("Add watcher cfg file modify err:", err)
	}

	<-stop
}

//PathExists 判断路径是否存在
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	if os.IsExist(err) {
		return true
	}

	return false
}

//CreateLogDir 创建目录路径
func CreateLogDir(logName string) (string, error) {
	// 获取当前路径
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	logpath := dir + "\\" + logName
	if PathExists(logpath) {
		return logpath, nil
	}

	err := os.Mkdir(logpath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("os.Mkdir err:%s", err.Error())
	}

	return logpath, nil
}

//GetCfgPath 获取当前配置文件的路径
func GetCfgPath() (string, error) {
	// 获取当前路径
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	cfgpath := dir + "\\monitorCfg\\config.ini"
	if PathExists(cfgpath) {
		return cfgpath, nil
	}

	if !PathExists(dir) {
		os.MkdirAll(dir, os.ModePerm)
	}

	if !PathExists(dir + "\\monitorCfg") {
		os.Mkdir(dir+"\\monitorCfg", os.ModePerm)
	}

	if !PathExists(cfgpath) {
		file, err := os.Create(cfgpath)
		if err != nil {
			return cfgpath, fmt.Errorf("config %s not exists and create it fail:%s", cfgpath, err)
		}
		defer file.Close()

		initContent := "#[Machine] 当前机器的标识名称\r\n" +
			"#[SpecInfo] 指定具体监控服务名Name(x) 以及该服务重启时需发送的附件Attach(x)\r\n" +
			"#[PartInfo] 指定监控服务名Name(x),支持模糊匹配(即service1表示监控含有service1开头的所有服务)，支持!运算(即!service1表示不监控含有service1名开头的服务)\r\n" +
			"#[EmailInfo] 邮件配置信息\r\n" +
			"#[Timer] 定时任务配置,其中RefreshCfg表示多少秒刷新监控的service,改参数修改需要重启服务后生效\r\n\n" +
			"[Machine]\r\nName=TradeA\r\n\n" +
			"[SpecInfo]\r\nName1=myservice\r\nAttach1=D:\\MyService\\Log\r\n\n" +
			"[PartInfo]\r\nName1=Doo_\r\nName2=!Doo_MonitorService\r\n\n" +
			"[EmailInfo]\r\nOpen=0\r\nHost=smtp.qq.com\r\nPort=25\r\nSendU=eamil@qq.com\r\nSendP=password\r\nReceiveU=email1@163.com,email2@qq.com\r\n\n" +
			"[Timer]\r\nRefresh=300"

		file.WriteString(initContent)
	}

	return cfgpath, nil
}

//IsDir 判断所给路径是否为文件夹
func IsDir(path string) bool {
	s, err := os.Stat(path)
	if err != nil {
		return false
	}
	return s.IsDir()
}

//GetAttachByPath 获取指定目录下的附件
func GetAttachByPath(path string) (attach string) {
	if !IsDir(path) {
		return path
	}

	if !PathExists(path) {
		logdoo.ErrorDoo("path", path, "no exist")
		return ""
	}

	attach, err := GetLastModFilesByPath(path)
	if err != nil {
		logdoo.ErrorDoo("GetLastModFilesByPath err:", err, "attach path", path)
		return ""
	}

	return attach
}

//GetLastModFilesByPath 获取指定目录下最后修改的文件
func GetLastModFilesByPath(dirPth string) (files string, err error) {
	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		return "", err
	}

	PthSep := string(os.PathSeparator)
	var tempFile os.FileInfo
	for i, fi := range dir {
		if !fi.IsDir() {
			if i == 0 {
				tempFile = fi
			} else {
				if tempFile.ModTime().Before(fi.ModTime()) {
					tempFile = fi
				}
			}
			files = dirPth + PthSep + tempFile.Name()
		}
	}

	return files, nil
}
