package main

import (
	"doofile"
	"fmt"
	"logdoo"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify"
)

const (
	WatcherModify = 1
	WatcherStop   = 2
)

const (
	ServiceCfgChange = 1
	FileCfgChange    = 2
	ComCfgChange     = 3
)

const (
	IsDebug   = 1
	IsRelease = 2
)

var monitorCfg = NewMonitorCfg()
var monitorService = NewMonitorService()
var monitorFile = NewMonitorFile()
var monitorEmail = NewEmail()

var logSer = logdoo.NewLogger()
var logFile = logdoo.NewLogger()

func main() {
	RunWindowService(IsDebug)
}

//mainMonitorService 监控服务主流程
func mainMonitorService() {
	//初始化日记
	if logPath, err := CreateLogDir("monitorServiceLog"); err == nil {
		var logSerF = logdoo.NewDayLogHandle(logPath, 800)
		var logSerC = logdoo.NewConsoleHandler()
		logSer.SetHandlers(logSerC, logSerF)
	} else {
		panic("CreateLogDir err")
	}

	if monitorCfg == nil {
		logSer.ErrorDoo("init monitor config file fail")
		return
	}

	logSer.InfoDoo("***monitor service start***")

	//获取配置文件目录
	cfgPath, err := GetCfgPath()
	if err != nil {
		logSer.ErrorDoo("GetCfgPath err", err)
		return
	}

	if err := LoadMonitorServiceCfg(monitorCfg, monitorService, monitorEmail, cfgPath); err != nil {
		logSer.ErrorDoo(err)
	}

	//配置每个日记文件的最大的大小
	if monitorCfg.logFileSize > 0 {
		logSer.SetDayLogHandleFileSize(monitorCfg.logFileSize)
	}

	monitorService.StartMonitor(monitorCfg, monitorEmail)
}

//mainMonitorFile 监控文件
func mainMonitorFile() {
	//初始化日记
	if logPath, err := CreateLogDir("monitorFileLog"); err == nil {
		var logFileF = logdoo.NewDayLogHandle(logPath, 800)
		var logFileC = logdoo.NewConsoleHandler()
		logFile.SetHandlers(logFileC, logFileF)
	} else {
		panic("CreateLogDir err")
	}

	if monitorCfg == nil {
		logFile.ErrorDoo("init monitor config file fail")
		return
	}

	logFile.InfoDoo("***monitor service start***")

	//获取配置文件目录
	cfgPath, err := GetCfgPath()
	if err != nil {
		logFile.ErrorDoo("GetCfgPath err", err)
		return
	}

	if err := monitorCfg.LoadMonitorFileCfg(cfgPath); err != nil {
		logFile.ErrorDoo("LoadMonitorFileCfg err:", err)
	}

	//配置每个日记文件的最大的大小
	if monitorCfg.logFileSize > 0 {
		logFile.SetDayLogHandleFileSize(monitorCfg.logFileSize)
	}

	paths := monitorCfg.GetMonitorFileList() //获取监控文件列表
	paths = RemoveRep(paths)                 //去除重复文件

	monitorFile.AddMonitorFile(paths)
	go monitorFile.StartWatcher() //开始监控，并阻塞直到退出
}

//WaitReloadCfg 配置文件修改时进行配置文件重载
func WaitReloadCfg() {

	//获取配置文件目录
	cfgPath, err := GetCfgPath()
	if err != nil {
		logSer.ErrorDoo("GetCfgPath err", err)
		return
	}

	hasModify := make(chan int)
	defer close(hasModify)

	timer := time.NewTicker(time.Duration(monitorCfg.GetRefreshTime()) * time.Second) //默认是5分钟刷新一次
	defer timer.Stop()

	go WatchCfgFile(cfgPath, hasModify)

	for {
		select {
		case event := <-hasModify:
			switch event {
			case ServiceCfgChange: //服务监控配置有修改
				logSer.InfoDoo("ServiceCfgChange")
				if err := UpdateMonitorServiceCfg(monitorCfg, monitorService, monitorEmail, cfgPath); err != nil {
					logSer.ErrorDoo(err)
				}

			case FileCfgChange: //文件监控配置有修改
				logFile.InfoDoo("FileCfgChange")

			case ComCfgChange: //公共配置有修改
				logFile.InfoDoo("CommonCfgChange")
				logSer.InfoDoo("CommonCfgChange")
				if err := monitorCfg.LoadMonitorComCfg(cfgPath); err == nil {
					monitorEmail.UpdateEmail(monitorCfg.GetEmailData()) //更新邮件配置
				} else {
					logFile.ErrorDoo("LoadMonitorComCfg err:", err)
					logSer.ErrorDoo("LoadMonitorComCfg err:", err)
				}

			default:
				logFile.InfoDoo("WaitReloadCfg unknow event modify")
				logSer.InfoDoo("WaitReloadCfg unknow event modify")
			}

		case <-timer.C: //定时刷新服务监控列表
			if err := UpdateMonitorServiceByCfg(monitorCfg, monitorService); err != nil {
				logSer.ErrorDoo(err)
			}

		case <-monitorService.stopChan: //监控服务退出释放资源
			monitorService.Release()
			break
		}
	}
}

//CloseService 关闭所有服务并释放资源
func CloseService() {
	monitorService.stopChan <- true
	monitorFile.stopChan <- true
	logSer.InfoDoo("***monitor service close***")
	logFile.InfoDoo("***monitor service close***")
	if logSer != nil {
		logSer.Close()
	}
	if logFile != nil {
		logFile.Close()
	}
}

//WatchCfgFile 监控配置文件是否有被修改了
func WatchCfgFile(cfgPath string, hasModify chan<- int) {

	//new 一个文件监控对象
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logSer.WarnDoo("watcher cfg file modify err:", err)
	}
	defer watcher.Close()

	//new 一个ini文件监控对象
	var mf = NewIniFileMonitor()
	cfgFileMonitor := doofile.NewIniFile()
	if _, err := cfgFileMonitor.LoadMonitorIniFiles([]string{cfgPath}); err != nil {
		logFile.InfoDoo(err)
	}
	mf.SetIniFileMonitor(cfgFileMonitor)

	//cfg 文件监控停止通知
	stop := make(chan int)
	defer close(stop)

	//cfg 文件变化通知处理(使用chan给对比监控协程传递新老文件名进行对比cfg文件当前的修改)
	names := make(chan [2]string)
	defer close(names)
	go HandleCfgChange(names, mf, hasModify) //协程中有进行延时处理,保证是秒级的对比cfg文件新老差异(因为像notepad++修改文件就会进行先删除后添加的操作，这会导致检查到的是整个cfg所有的配置都有变化了)

	//新老cfg配置文件名列表(只要不是重命名操作，新路径和老路径就都是一样的)
	oldNewName := [2]string{"", ""}

	//开协程监听文件是否别修改
	go func() {
		var oldName string
		var isRename = false
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					stop <- WatcherStop
					return
				}

				//重命名操作做记号处理
				if event.Op == fsnotify.Rename {
					oldName = event.Name
					isRename = true
				}

				//当前监控的配置文件有write操作了证明被修改了
				if event.Op&fsnotify.Write == fsnotify.Write {
					if !isRename {
						oldName = event.Name
					} else {
						isRename = false
					}

					//是ini Cfg文件就进行发信号处理对比当前cfg文件的修改了哪些内容
					if doofile.IsIniFile(oldName) && doofile.IsIniFile(event.Name) {
						oldNewName[0] = oldName
						oldNewName[1] = event.Name
						names <- oldNewName
					}
				}

			case _, ok := <-watcher.Errors:
				if !ok {
					stop <- WatcherStop
					return
				}
			}
		}
	}()

	if err := watcher.Add(cfgPath); err != nil {
		logSer.WarnDoo("Add watcher cfg file modify err:", err)
	}

	<-stop
}

//HandleCfgChange 处理配置文件的修改操作(通知是那部分的配置文件被修改了)
func HandleCfgChange(names <-chan [2]string, mf *IniFileMonitor, hasModify chan<- int) {

	timer := time.NewTimer(1 * time.Second)
	var nameOld string
	var nameNew string

	for {
		select {
		case n := <-names:
			nameOld = n[0]
			nameNew = n[1]
			timer.Reset(1 * time.Second)
		case <-timer.C:
			if len(nameOld) == 0 || len(nameNew) == 0 {
				continue
			}
			secMap := mf.GetChangeSection(nameOld, nameNew)
			if _, ok := secMap["MonitorServiceSpec"]; ok {
				hasModify <- ServiceCfgChange
			} else if _, ok := secMap["MonitorServicePart"]; ok {
				hasModify <- ServiceCfgChange
			}

			if _, ok := secMap["MonitorFileDir"]; ok {
				hasModify <- FileCfgChange
			} else if _, ok := secMap["MonitorFileSpec"]; ok {
				hasModify <- FileCfgChange
			}

			if _, ok := secMap["EmailInfo"]; ok {
				hasModify <- ComCfgChange
			} else if _, ok := secMap["CommonInfo"]; ok {
				hasModify <- ComCfgChange
			}

			nameOld = ""
			nameNew = ""
		}
	}
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
