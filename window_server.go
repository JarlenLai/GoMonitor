package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/btcsuite/winsvc/debug"
	"github.com/btcsuite/winsvc/eventlog"
	"github.com/btcsuite/winsvc/mgr"
	"github.com/btcsuite/winsvc/svc"
	"github.com/kardianos/service"
	"golang.org/x/sys/windows"
)

const (
	IDOK     = 1
	IDCANCEL = 2
	IDABORT  = 3
	IDRETRY  = 4
	IDIGNORE = 5
	IDYES    = 6
	IDNO     = 7
)

var elog debug.Log

const windowlogID uint32 = 6661

type program struct{}

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *program) run() {
	ServerMain()
}

func (p *program) Stop(s service.Service) error {
	CloseService()
	return nil
}

func RunWindowService(status int) {
	svcConfig := &service.Config{
		Name:        "Doo_MonitorService",     //服务显示名称
		DisplayName: "Doo_MonitorService",     //服务名称
		Description: "Monitor window service", //服务描述
	}

	if status == IsDebug {
		elog = debug.New(svcConfig.Name)
	} else {
		var err error
		elog, err = eventlog.Open(svcConfig.Name)
		if err != nil {
			return
		}
	}
	defer elog.Close()

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		elog.Error(windowlogID, fmt.Sprintf("service.New err:%v", err))
		return
	}

	//命令行方式操作服务
	if len(os.Args) > 1 {
		ServiceControl(s, os.Args[1])
		return
	}

	//对话框方式操作服务
	if status != IsDebug && WinServiceControl(s, svcConfig.Name) {
		return
	}

	err = s.Run()
	if err != nil {
		elog.Error(windowlogID, fmt.Sprintf("service run err:%v", err))

	}
}

//ServiceControl 命令行的方式控制服务
func ServiceControl(s service.Service, cmd string) {
	if cmd == "install" {
		if err := s.Install(); err != nil {
			elog.Error(windowlogID, fmt.Sprintf("service install err:%v", err))
		} else {
			elog.Info(windowlogID, "service install success")
		}
		return
	}

	if cmd == "uninstall" {
		if err := s.Uninstall(); err != nil {
			elog.Error(windowlogID, fmt.Sprintf("service uninstall err:%v", err))
		} else {
			elog.Info(windowlogID, fmt.Sprintf("service uninstall success"))
		}
		return
	}

	if cmd == "stop" {
		if err := s.Stop(); err != nil {
			elog.Error(windowlogID, fmt.Sprintf("service stop err:%v", err))
		} else {
			elog.Info(windowlogID, fmt.Sprintf("service stop success"))
		}
		return
	}

	if cmd == "start" {
		if err := s.Start(); err != nil {
			elog.Error(windowlogID, fmt.Sprintf("service start err:%v", err))
		} else {
			elog.Info(windowlogID, fmt.Sprintf("service start success"))
		}
		return
	}

	elog.Error(windowlogID, fmt.Sprintf("Unknow service cmd:%s", cmd))
	return
}

//WinServiceControl 对话框的方式控制服务
func WinServiceControl(s service.Service, serviceName string) (op bool) {
	manager, err := mgr.Connect()
	if err != nil {
		elog.Error(windowlogID, fmt.Sprintf("Open service manager err:%v", err))
		return true
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		cont, _ := syscall.UTF16FromString("install service")
		title, _ := syscall.UTF16FromString(serviceName)
		if ret, _ := windows.MessageBox(0, (*uint16)(unsafe.Pointer(&cont[0])), (*uint16)(unsafe.Pointer(&title[0])), windows.MB_ICONQUESTION|windows.MB_YESNO); ret == IDYES {
			ServiceControl(s, "install")
			if service, err := manager.OpenService(serviceName); err == nil {
				cont, _ = syscall.UTF16FromString("service install success\r\nDo you  want to start service now")
				title, _ = syscall.UTF16FromString(serviceName)
				if ret, _ := windows.MessageBox(0, (*uint16)(unsafe.Pointer(&cont[0])), (*uint16)(unsafe.Pointer(&title[0])), windows.MB_ICONQUESTION|windows.MB_YESNO); ret == IDYES {
					ServiceControl(s, "start")
				}
				service.Close()
			}

		}
		return true
	}
	defer service.Close()

	if status, er := service.Query(); er == nil {
		elog.Info(windowlogID, fmt.Sprintf("current service:%s state %d", serviceName, status.State))
		if status.State == svc.Stopped {
			cont, _ := syscall.UTF16FromString("是(Y)：uninstall service， 否(N)：则进入下一步 start servce")
			title, _ := syscall.UTF16FromString(serviceName)
			if ret, _ := windows.MessageBox(0, (*uint16)(unsafe.Pointer(&cont[0])), (*uint16)(unsafe.Pointer(&title[0])), windows.MB_ICONQUESTION|windows.MB_YESNO); ret == IDYES {
				ServiceControl(s, "uninstall")
			} else {
				cont, _ = syscall.UTF16FromString("start service")
				title, _ = syscall.UTF16FromString(serviceName)
				if ret, _ := windows.MessageBox(0, (*uint16)(unsafe.Pointer(&cont[0])), (*uint16)(unsafe.Pointer(&title[0])), windows.MB_ICONQUESTION|windows.MB_YESNO); ret == IDYES {
					ServiceControl(s, "start")
				}
			}
		} else if status.State == svc.Running {
			cont, _ := syscall.UTF16FromString("stop service")
			title, _ := syscall.UTF16FromString(serviceName)
			if ret, _ := windows.MessageBox(0, (*uint16)(unsafe.Pointer(&cont[0])), (*uint16)(unsafe.Pointer(&title[0])), windows.MB_ICONQUESTION|windows.MB_YESNO); ret == IDYES {
				ServiceControl(s, "stop")
			}
		} else if status.State == svc.StartPending {
			return false //服务正在启动中(即手动在任务管理器中启动服务时的情况)
		}
	}

	return true
}
