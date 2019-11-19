package main

import (
	"doofile"
	"fmt"
	"logdoo"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify"
)

const (
	ServiceCfgChange = 1 //监控服务配置改变
	FileCfgChange    = 2 //监控文件配置改变
	ComCfgChange     = 3 //公共配置改变
)

const (
	IsDebug   = 1 //交互模式，debug
	IsRelease = 2 //非交互模式，即window service模式
)

var RunMode int //运行模式，即IsDebug或者IsRelease

var stopServiceChan = make(chan bool) //主进程退出通知

var monitorCfg = NewMonitorCfg()         //监控当前程序的配置文件的变化
var monitorService = NewMonitorService() //监控当前机器的window service
var monitorFile = NewMonitorFile()       //监控文件的修改记录
var monitorEmail = NewEmail()            //邮件功能

var logSer = logdoo.NewLogger()  //监控服务log函数
var logFile = logdoo.NewLogger() //监控文件log函数

//函数入口
func main() {
	RunMode = IsRelease //如果要调试或者本地运行,请修改这里
	RunWindowService(RunMode)
}

//mainMonitorService 监控服务主流程
func mainMonitorService() {
	//初始化日记
	if logPath, err := CreateLogDir("monitorServiceLog"); err == nil {
		var logSerF = logdoo.NewDayLogHandle(logPath, 800)
		if RunMode == IsDebug {
			var logSerC = logdoo.NewConsoleHandler()
			logSer.SetHandlers(logSerC, logSerF)
		} else {
			logSer.SetHandlers(logSerF) //不是交互模式不必打印信息到控制台
		}

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
		if RunMode == IsDebug {
			var logFileC = logdoo.NewConsoleHandler()
			logFile.SetHandlers(logFileC, logFileF)
		} else {
			logFile.SetHandlers(logFileF) //不是交互模式不必打印信息到控制台
		}
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
	monitorFile.StartWatcher(paths)          //开始监控，并阻塞直到退出
}

//WaitReloadCfg 配置文件修改时进行配置文件重载
func WaitReloadCfg() {

	//获取配置文件目录
	cfgPath, err := GetCfgPath()
	if err != nil {
		logSer.ErrorDoo("GetCfgPath err", err)
		return
	}

	//配置文件有被修改的通知
	hasModify := make(chan int)
	defer close(hasModify)

	//定时刷新SCM任务管理器的定时器(针对新安装服务及时添加进入监控列表而无需重启服务)
	timer := time.NewTicker(time.Duration(monitorCfg.GetRefreshSCMTime()) * time.Second) //默认是5分钟刷新一次
	defer timer.Stop()

	//启动配置文件监控协程
	watcher := WatchCfgFile(cfgPath, hasModify)

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
				if err := UpdateMonitorFileCfg(monitorCfg, monitorFile, monitorEmail, cfgPath); err != nil {
					logFile.ErrorDoo(err)
				}

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

		case <-stopServiceChan: //整个服务退出
			watcher.Close()
			return
		}
	}
}

//CloseService 关闭所有服务并释放资源(资源释放后面还需考虑一下)
func CloseService() {
	logSer.InfoDoo("***monitor service close***")
	logFile.InfoDoo("***monitor service close***")

	stopServiceChan <- true
	monitorService.stopChan <- true
	monitorFile.stopChan <- true

	monitorService.Release()
	monitorFile.Release()

	if logSer != nil {
		logSer.Close()
	}
	if logFile != nil {
		logFile.Close()
	}
}

//WatchCfgFile 监控配置文件是否有被修改了, 返回一个监控对象
func WatchCfgFile(cfgPath string, hasModify chan<- int) *fsnotify.Watcher {

	//new 一个文件监控对象,Watcher对象由外部调用者关闭
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logSer.WarnDoo("watcher cfg file modify err:", err)
		return nil
	}

	//new 一个ini文件监控对象
	var mf = NewIniFileMonitor()
	cfgFileMonitor := doofile.NewIniFile()
	if _, err := cfgFileMonitor.LoadMonitorIniFiles([]string{cfgPath}); err != nil {
		logFile.InfoDoo(err)
	}
	mf.SetIniFileMonitor(cfgFileMonitor)

	//cfg 文件变化通知处理(使用chan给对比监控协程传递新老文件名进行对比cfg文件当前的修改)
	names := make(chan [2]string)
	go HandleCfgChange(names, mf, hasModify) //协程中有进行延时处理,保证是秒级的对比cfg文件新老差异(因为像notepad++修改文件就会进行先删除后添加的操作，这会导致检查到的是整个cfg所有的配置都有变化了)

	//开协程监听文件是否别修改
	go func(ns chan [2]string, wt *fsnotify.Watcher) {
		var oldName string
		var isRename = false
		oldNewName := [2]string{"", ""} //新老cfg配置文件名列表(只要不是重命名操作，新路径和老路径就都是一样的)
		for {
			select {
			case event, ok := <-wt.Events:
				if !ok {
					close(names) //如果退出就释放子协程
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
						ns <- oldNewName
					}
				}

			case _, ok := <-watcher.Errors:
				if !ok {
					close(names) //如果退出就释放子协程
					return
				}
			}
		}
	}(names, watcher)

	if err := watcher.Add(cfgPath); err != nil {
		logSer.WarnDoo("Add watcher cfg file modify err:", err)
	}

	return watcher
}

//HandleCfgChange 处理配置文件的修改操作(通知是那部分的配置文件被修改了)
func HandleCfgChange(names <-chan [2]string, mf *IniFileMonitor, hasModify chan<- int) {

	timer := time.NewTimer(1 * time.Second)
	var nameOld string
	var nameNew string

	for {
		select {
		case n, ok := <-names:
			if !ok {
				timer.Stop()
				return //主协程已经退出，该子协程也要退出
			}
			nameOld = n[0]
			nameNew = n[1]
			timer.Reset(1 * time.Second)
		case <-timer.C:
			if len(nameOld) == 0 || len(nameNew) == 0 {
				continue
			}

			secMap := mf.GetChangeSection(nameOld, nameNew) //获取是配置文件的那部分配置有修改了并进行通知
			for k := range secMap {
				if strings.HasPrefix(k, "MonitorService") {
					hasModify <- ServiceCfgChange
				} else if strings.HasPrefix(k, "MonitorFile") {
					hasModify <- FileCfgChange
				} else if strings.HasPrefix(k, "Common") {
					hasModify <- ComCfgChange
				}
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

		initContent := "#公共配置CommonXXX\r\n" +
			"#<MachineName> 当前机器的标识名称(非必填,用于邮件通知功能的标题)\r\n" +
			"#<LogFileSize> 每个日记文件的大小(单位M)，超过该阀值的日记文件就会进行重命名_(n)\r\n" +
			"#[CommonEmail] 邮件配置信息(非必填，open=0不开启用邮件通知功能, open=1开启邮件通知功能)\r\n" +
			"[CommonData]\r\nMachineName=Trade_A\r\nLogFileSize=800\r\n" +
			"[CommonEmail]\r\nOpen=0\r\nHost=smtp.qq.com\r\nPort=25\r\nSendU=eamil@qq.com\r\nSendP=password\r\nReceiveU=email1@163.com,email2@qq.com\r\n\n" +

			"#服务监控配置MonitorServiceXXX\r\n" +
			"#[MonitorServiceSpec] 指定具体监控服务名Name(n) 以及该服务重启时需发送的附件Attach(n)\r\n" +
			"#[MonitorServicePart] 指定监控服务名Name(n),支持模糊匹配(即service1表示监控含有service1开头的所有服务)，支持!运算(即!service1表示不监控含有service1名开头的服务)\r\n" +
			"#<RefreshSCMTime> 定时多少秒刷新任务管理器中监控列表的时间(主要针对新安装服务在安装后不用重启监控服务即可加入监控的功能),该值需重启生效。建议调大点(毕竟不经常安装服务)\r\n" +
			"[MonitorServiceSpec]\r\nName1=service1\r\nAttach1=F:\\service1\\crash\r\n" +
			"[MonitorServicePart]\r\nName1=!Doo_MonitorService\r\nName2=Doo_\r\n" +
			"[MonitorServiceTimer]\r\nRefreshSCMTime=300\r\n\n" +

			"#文件监控配置MonitorFileXXX(切记不要监控本程序自身的日记文件会导致死循环监控)\r\n" +
			"#[MonitorFileDir]Type(n)+Path(n)=要监控的具体文件\r\n" +
			"#[MonitorSpecFile]特别指定的监控文件\r\n" +
			"[MonitorFileDir]\r\nType1=ini,exe\r\nPath1=F:\\Monitor\r\nType2=ini,exe\r\nPath2=F:\\Monitor\r\n" +
			"[MonitorFileSpec]\r\nFile1=F:\\Monitor\\Jarlen1.txt\r\nFile3=F:\\Monitor\\Jarlen3.txt\r\n"

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
