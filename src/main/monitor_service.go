package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/btcsuite/winsvc/mgr"
	"github.com/btcsuite/winsvc/svc"
	"golang.org/x/sys/windows"
)

const (
	ServiceChanNum = 10
)

const (
	ServiceStoped  = 1
	ServicePending = 2
	ServiceRuning  = 3
	ServiceUnknow  = 4
)

type MonitorService struct {
	scm                 *mgr.Mgr                //任务管理器连接
	services            map[string]*mgr.Service //serviceName 与 实例句柄的映射
	serviceEmail        map[string]bool         //当前服务监控过程是否已经发送过邮件通知了
	serviceState        map[string]int          //记录当前服务的状态
	serviceAddChan      []chan mgr.Service      //处理要监控的service的chan队列,当前要重启那个service就把对应的service放入改chan中
	serviceAddChanIndex map[int]bool            //记录改service的chan是否有在处理中
	curAddChanIndex     int                     //记录最后一个用到的service的chan
	serviceDelChan      chan mgr.Service        //处理当前要移除那个service的chan队列,要移除对那个service的监控就把该service放入这个chan中
	stop                bool                    //监控功能是否停止了
	stopChan            chan bool               //停止监控通知
	mu                  sync.RWMutex
}

func NewMonitorService() *MonitorService {
	manager, err := mgr.Connect()
	if err != nil {
		logSer.ErrorDoo("NewMonitorService fail to open mgr err", err)
		return nil
	}
	return &MonitorService{scm: manager,
		services:            make(map[string]*mgr.Service),
		serviceEmail:        make(map[string]bool),
		serviceState:        make(map[string]int),
		serviceAddChan:      make([]chan mgr.Service, ServiceChanNum),
		serviceAddChanIndex: make(map[int]bool, ServiceChanNum),
		curAddChanIndex:     0,
		serviceDelChan:      make(chan mgr.Service, 100),
		stop:                false,
		stopChan:            make(chan bool)}
}

//LoadMonitorServiceCfg 根据配置文件加载监控服务信息
func LoadMonitorServiceCfg(mc *MonitorCfg, ms *MonitorService, e *Email, cfgPath string) error {
	if err := mc.LoadMonitorServiceCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadMonitorServiceCfg err:%s", err)
	}

	if err := mc.LoadMonitorComCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadMonitorComCfg err:%s", err)
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
	logSer.InfoDoo(fmt.Sprintf("LoadMonitorServiceCfg cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//UpdateMonitorServiceCfg 配置文件修改时更新同时更新监控数据
func UpdateMonitorServiceCfg(mc *MonitorCfg, ms *MonitorService, e *Email, cfgPath string) error {
	if err := mc.LoadMonitorServiceCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadMonitorServiceCfg err:%s", err)
	}

	specServices := mc.GetSpecServices()
	partServices := mc.GetPartServices()
	ms.UpdateServices(specServices, partServices)
	services := ms.GetMointorServices()
	var str string
	for _, service := range services {
		str += service + "\n"
	}
	logSer.InfoDoo(fmt.Sprintf("UpdateMonitorServiceCfg cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//UpdateMonitorServiceByCfg 用于定时任务定时刷新任务管理器中需要监控的服务
func UpdateMonitorServiceByCfg(mc *MonitorCfg, ms *MonitorService) error {

	specServices := mc.GetSpecServices()
	partServices := mc.GetPartServices()
	ms.UpdateServices(specServices, partServices)
	services := ms.GetMointorServices()
	var str string
	for _, service := range services {
		str += service + "\n"
	}
	logSer.InfoDoo(fmt.Sprintf("UpdateMonitorServiceByCfg cur monitor services:%d\r\n%s", len(services), str))

	return nil
}

//StartMonitor 开始监控功能
func (ms *MonitorService) StartMonitor(c *MonitorCfg, e *Email) {

	for i := 0; i < ServiceChanNum; i++ {
		ms.serviceAddChan[i] = make(chan mgr.Service)
		ms.serviceAddChanIndex[i] = false
	}

	for i := 0; i < ServiceChanNum; i++ {
		go ms.Addmonitor(i, c, e)
	}

	go ms.DelMonitor()

	go func(ms *MonitorService) {
		ok := true
		for ok {
			ms.mu.RLock()
			if ms.stop {
				ok = false
			}
			ms.mu.RUnlock()

			ms.LoopCheck()
			ms.RefreshServiceHandle()
			time.Sleep(10 * time.Millisecond)
		}
	}(ms)
}

//LoopCheck 轮训检查一遍服务
func (ms *MonitorService) LoopCheck() {
	var sers = make([]mgr.Service, 0)
	ms.mu.Lock()
	for _, service := range ms.services {
		if service == nil {
			continue
		}

		//如果服务正在使用API StartService 启动服务,那么下面的查询该服务的状态会导致阻塞的。
		if ms.serviceState[service.Name] == ServicePending {
			continue
		}

		status, err := service.Query()
		if err != nil {
			logSer.ErrorDoo("Query service", service.Name, "err", err)
			if v, ok := ms.services[service.Name]; ok {
				if v != nil {
					ms.services[service.Name].Close()
					ms.services[service.Name] = nil
				}
			}
			continue
		}

		if status.State == svc.Stopped {
			sers = append(sers, *service)
			ms.serviceState[service.Name] = ServicePending
		}
	}
	ms.mu.Unlock()

	for _, service := range sers {
		ms.serviceAddChan[ms.GetIdleChanIndex()] <- service
	}
}

//GetIdleChanIndex 获取空闲的ChanIndex(内部会死循环直到能获取到)
func (ms *MonitorService) GetIdleChanIndex() int {
	ok := true
	v := false
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for ok {
		if v, ok = ms.serviceAddChanIndex[ms.curAddChanIndex]; ok {
			if v == false {
				index := ms.curAddChanIndex
				ms.serviceAddChanIndex[index] = true
				ms.curAddChanIndex = (ms.curAddChanIndex + 1) % ServiceChanNum
				return index
			}
			ms.curAddChanIndex = (ms.curAddChanIndex + 1) % ServiceChanNum
		}
	}

	return 0
}

//RefreshServiceHandle 刷新监控服务的操作句柄
func (ms *MonitorService) RefreshServiceHandle() {

	//没有停止的服务不需要刷新
	if len(ms.GetStopServices()) == 0 {
		return
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.scm == nil {
		manager, err := mgr.Connect()
		if err == nil {
			ms.scm = manager
		} else {
			logSer.ErrorDoo("Open service manager err", err)
			return
		}
	}

	for k, v := range ms.services {
		if v == nil {
			service, err := ms.scm.OpenService(k)
			if err == nil {
				ms.services[k] = service
			}
		}
	}
}

//RefreshMgrHandle 刷新连接任务管理器的句柄
func (ms *MonitorService) RefreshMgrHandle() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.scm != nil {
		ms.scm.Disconnect()
		ms.scm = nil
		manager, err := mgr.Connect()
		if err == nil {
			ms.scm = manager
		}
	} else {
		manager, err := mgr.Connect()
		if err == nil {
			ms.scm = manager
		}
	}
}

//Release 释放资源
func (ms *MonitorService) Release() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for k, v := range ms.services {
		if v != nil {
			v.Close()
			delete(ms.services, k)
		}
	}

	if ms.scm != nil {
		ms.scm.Disconnect()
		ms.scm = nil
	}

	ms.stop = true

	for i := 0; i < ServiceChanNum; i++ {
		close(ms.serviceAddChan[i])
	}

	close(ms.serviceDelChan)
}

//AddSpecService 添加要监控的具体服务名列表
func (ms *MonitorService) AddSpecService(names []string) *[]string {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.scm == nil {
		manager, err := mgr.Connect()
		if err == nil {
			ms.scm = manager
		} else {
			logSer.ErrorDoo("Open service manager err", err)
			return nil
		}
	}

	for _, name := range names {

		//已经存在的就不用添加了
		if _, ok := ms.services[name]; ok {
			continue
		}

		//不存在的就添加
		service, err := ms.scm.OpenService(name)
		if err != nil {
			ms.services[name] = nil
			ms.serviceState[name] = ServiceStoped
			logSer.WarnDoo("service:", name, "open err:", err)
		} else {
			ms.services[name] = service
			ms.serviceState[name] = ServiceUnknow
		}
	}

	return &names
}

//AddPartService 添加要监控的对应前缀的服务列表
func (ms *MonitorService) AddPartService(names []string) *[]string {

	var monitorNames = make([]string, 0)
	var noMonitorNames = make([]string, 0)

	for _, name := range names {
		if strings.HasPrefix(name, "!") {
			name = strings.TrimPrefix(name, "!")
			noMonitorNames = append(noMonitorNames, name) //不需要监控的匹配名
		} else {
			monitorNames = append(monitorNames, name) //需要监控的匹配名
		}
	}

	manager, err := mgr.Connect()
	if err != nil {
		logSer.ErrorDoo("Open service manager err", err)
		return nil
	}
	defer manager.Disconnect()

	var needBuf uint32
	var serviceNum uint32
	if err := windows.EnumServicesStatusEx(windows.Handle(manager.Handle), windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_WIN32, windows.SERVICE_STATE_ALL, nil, 0, &needBuf, &serviceNum, nil, nil); err != nil {
		//这里会报错但是不影响到获取needBuf
	}

	services := make([]byte, needBuf)
	if err := windows.EnumServicesStatusEx(windows.Handle(manager.Handle), windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_WIN32, windows.SERVICE_STATE_ALL, (*byte)(unsafe.Pointer(&services[0])), needBuf, &needBuf, &serviceNum, nil, nil); err != nil {
		logSer.ErrorDoo("EnumServicesStatusEx get part service list fail err:", err)
		return nil
	}

	var sizeWinSer windows.ENUM_SERVICE_STATUS_PROCESS
	iter := uintptr(unsafe.Pointer(&services[0]))
	curPartSerList := make([]string, 0)
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for i := uint32(0); i < serviceNum; i++ {
		var data = (*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(iter))
		iter = uintptr(unsafe.Pointer(iter + unsafe.Sizeof(sizeWinSer)))
		//fmt.Printf("Service Name: %s - Display Name: %s - %#v\r\n", syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(data.ServiceName))[:]), syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(data.DisplayName))[:]), data.ServiceStatusProcess)
		name := syscall.UTF16ToString((*[100]uint16)(unsafe.Pointer(data.ServiceName))[:])

		//排除不要监控的文件名前缀的服务
		filter := false
		for _, n := range noMonitorNames {
			if strings.HasPrefix(name, n) {
				filter = true
				break
			}
		}
		if filter {
			continue
		}

		//只监控对应需要监控前缀名的服务
		match := false
		for _, n := range monitorNames {
			if strings.HasPrefix(name, n) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		curPartSerList = append(curPartSerList, name)

		//原本已经在监控的就不用再添加了
		if _, ok := ms.services[name]; ok {
			continue
		}

		//把服务加入监控列表
		s, err := manager.OpenService(name)
		if err != nil {
			ms.services[name] = nil
			ms.serviceState[name] = ServiceStoped
			logSer.WarnDoo("service:", name, "maybe not exist and open err:", err)
		} else {
			ms.services[name] = s
			ms.serviceState[name] = ServiceUnknow
		}
	}

	return &curPartSerList
}

//AddserviceAttach 添加服务对应的邮件附件路径目录(当服务重启的时候可能需要把附件发送进行通知)
func (ms *MonitorService) AddserviceAttach(names []string) {
	ms.mu.Lock()
	for _, name := range names {
		if _, ok := ms.serviceEmail[name]; !ok {
			ms.serviceEmail[name] = false
		}
	}
	ms.mu.Unlock()
}

//UpdateServices 更新监控列表
func (ms *MonitorService) UpdateServices(specs, parts []string) {

	services := make(map[string]*int, 0)
	if specServices := ms.AddSpecService(specs); specServices != nil {
		for _, v := range *specServices {
			services[v] = nil
		}
	}

	if partServices := ms.AddPartService(parts); partServices != nil {
		for _, v := range *partServices {
			services[v] = nil
		}
	}

	ms.mu.Lock()
	for k := range ms.services {
		if _, ok := services[k]; !ok {
			if ms.services[k] == nil {
				delete(ms.services, k) //空资源的直接删除就好了
				continue
			}
			ms.serviceDelChan <- *ms.services[k] //使用协程的方式去关闭释放一下不要监控的服务资源(因为有些本来正在启动中的服务，现在不需要监控了关闭系统句柄资源时会进行阻塞)
			delete(ms.services, k)
		}
	}
	ms.mu.Unlock()
}

//GetMointorServices 获取当前在监控的服务列表
func (ms *MonitorService) GetMointorServices() (services []string) {
	ms.mu.RLock()
	for k := range ms.services {
		services = append(services, k)
	}
	ms.mu.RUnlock()

	return services
}

//GetStopServices 获取当前已经停止的服务列表
func (ms *MonitorService) GetStopServices() (services []string) {
	ms.mu.RLock()
	for k, v := range ms.services {
		if v == nil {
			services = append(services, k)
		}
	}
	ms.mu.RUnlock()

	return services
}

//Addmonitor 处理需要尝试启动的服务
func (ms *MonitorService) Addmonitor(i int, c *MonitorCfg, e *Email) {

	if i >= ServiceChanNum {
		return
	}

	var curState = ServiceUnknow
	for {
		select {
		case service := <-ms.serviceAddChan[i]:
			ms.SendEmail(service.Name, c, e)
			logSer.InfoDoo("goroutine", i, "begin restart service", service.Name)
			//service.Start 这个函数是阻塞式的,没有及时响应会导致30秒后超时
			if er := service.Start([]string{service.Name}); er != nil {
				logSer.ErrorDoo("goroutine", i, "restart service", service.Name, "err", er)
				curState = ServiceStoped
			} else {
				logSer.InfoDoo("goroutine", i, "restart service", service.Name, "success")
				ms.UpdateSendEmailState(service.Name, false)
				curState = ServiceRuning
			}

			ms.mu.Lock()
			if _, ok := ms.services[service.Name]; ok {
				ms.serviceAddChanIndex[i] = false
				ms.serviceState[service.Name] = curState
			}
			ms.mu.Unlock()
		}
	}
}

//DelMonitor 删除监控释放资源
func (ms *MonitorService) DelMonitor() {
	for {
		select {
		case service := <-ms.serviceDelChan:
			logSer.InfoDoo("delete service:", service.Name, "monitor")
			service.Close()
		}
	}
}

//SendEmail 发送邮件
func (ms *MonitorService) SendEmail(name string, c *MonitorCfg, e *Email) {
	if e == nil {
		logSer.WarnDoo("email instance is nil and can't send email please confirm!")
		return
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	//已经发送过的不再发送了
	if _, ok := ms.serviceEmail[name]; ok {
		if ms.serviceEmail[name] {
			return
		}
	}

	//发送邮件通知有两种(有附件和没附件)
	if attach, ok := c.GetServiceAttachPath(name); ok {
		if GetAttachByPath(attach) != "" {
			subject := "machine:" + c.GetMachineName() + " service: " + name + " has stop and restart!"
			content := "<b>The crash file please the attach</b>"
			if err, ok := e.SendEmailEx(subject, content, GetAttachByPath(attach)); err != nil {
				logSer.InfoDoo(err)
			} else {
				if ok {
					logSer.InfoDoo("send eamilEx ex success ==>> From:", e.GetSendU(), "To:", e.GetReceiveU(), "subject:", subject, "content:", content, "attach:", attach)
				}
			}
			ms.serviceEmail[name] = true
			return
		}
	}

	subject := "machine:" + c.GetMachineName() + " service: " + name + " has stop and restart!"
	content := "<b>please handle</b>"
	if err, ok := e.SendEmail(subject, content); err != nil {
		logSer.InfoDoo(err)
	} else {
		if ok {
			logSer.InfoDoo("send eamilEx ex success ==>> From:", e.GetSendU(), "To:", e.GetReceiveU(), "subject:", subject, "content:", content)
		}
	}
	ms.serviceEmail[name] = true
}

//UpdateSendEmailState 更新发送邮件状态
func (ms *MonitorService) UpdateSendEmailState(name string, state bool) {
	ms.mu.Lock()
	if _, ok := ms.serviceEmail[name]; ok {
		ms.serviceEmail[name] = state
	}
	ms.mu.Unlock()
}

//GetAttachByPath 获取指定目录下的附件
func GetAttachByPath(path string) (attach string) {
	if !IsDir(path) {
		return path
	}

	if !PathExists(path) {
		logSer.ErrorDoo("path", path, "no exist")
		return ""
	}

	attach, err := GetLastModFilesByPath(path)
	if err != nil {
		logSer.ErrorDoo("GetLastModFilesByPath err:", err, "attach path", path)
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
