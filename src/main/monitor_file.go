package main

import (
	"doofile"
	"sync"
	"time"

	"github.com/fsnotify"
)

//MonitorFile 监控的文件列表
type MonitorFile struct {
	fileMap  map[string]bool
	fileList []string
	stopChan chan bool
}

//IniFileMonitor 监控ini文件
type IniFileMonitor struct {
	iniFile *doofile.IniFile
	mu      sync.Mutex
}

var myIniFile = NewIniFileMonitor()

//NewMonitorFile New一个监控文件列表存储结构变量
func NewMonitorFile() *MonitorFile {
	m := &MonitorFile{make(map[string]bool), make([]string, 0), make(chan bool)}
	return m
}

//NewIniFileMonitor New一个监控ini文件的存储结构信息
func NewIniFileMonitor() *IniFileMonitor {
	iniFile := &IniFileMonitor{}
	return iniFile
}

//AddMonitorFile 添加监控文件列表
func (files *MonitorFile) AddMonitorFile(paths []string) {
	files.fileList = append(files.fileList, paths...)
	return
}

//StartWatcher 开始进行监控
func (files *MonitorFile) StartWatcher() {

	iniMonitor := doofile.NewIniFile()
	if _, err := iniMonitor.LoadMonitorIniFiles(files.fileList); err != nil {
		logFile.InfoDoo(err)
	}
	myIniFile.SetIniFileMonitor(iniMonitor)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logFile.InfoDoo("fsnotify.NewWatcher err:", err)
	}
	defer watcher.Close()

	go func() {
		var oldName string
		var isRename = false
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op == fsnotify.Rename {
					oldName = event.Name
					isRename = true
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					if !isRename {
						oldName = event.Name
					} else {
						isRename = false
					}
					//目前只有ini文件的监控会记录修改差异,后续需要扩展其他的文件类型监控差异在此修改
					if doofile.IsIniFile(oldName) && doofile.IsIniFile(event.Name) {
						myIniFile.PrintDiff(oldName, event.Name)
					} else {
						if len([]rune(event.Name)) > 0 {
							logFile.InfoDoo("monitor event:", event)
						}

					}

				} else {
					if len([]rune(event.Name)) > 0 {
						logFile.InfoDoo("monitor event:", event)
					}
				}

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
				//这里有个bug https://github.com/fsnotify/fsnotify/issues/72
				//logFile.ErrorDoo("monitor event:", err)
			}
		}
	}()

	//添加监控文件通知
	iniFiles := make([]string, 0)
	for _, path := range files.fileList {
		err = watcher.Add(path)
		if err != nil {
			logFile.ErrorDoo("add watch file:", path, "err:", err)
			files.fileMap[path] = false
			continue
		}
		iniFiles = append(iniFiles, path)
		files.fileMap[path] = true
	}

	logFile.InfoDoo("cur monitor file num:", len(iniFiles))
	var str string
	for _, iniFile := range iniFiles {
		str += iniFile + "\n"
	}
	logFile.InfoDoo("monifile file list:\r\n", str)

	//定时检查并对添加监控失败的文件进行重新监控
	go func() {
		timer := time.NewTicker(60 * time.Second)
		for {
			select {
			case <-timer.C:
				for k, v := range files.fileMap {
					if !v && PathExists(k) {
						err = watcher.Add(k)
						if err != nil {
							logFile.ErrorDoo("add watch file:", k, "err:", err)
							files.fileMap[k] = false
						}
						files.fileMap[k] = true
					} else if v && !PathExists(k) {
						files.fileMap[k] = false
					}
				}
			}
		}
	}()

	<-files.stopChan
}

//SetIniFileMonitor 赋值监控的ini文件列表信息
func (iniFile *IniFileMonitor) SetIniFileMonitor(monitorIniFile *doofile.IniFile) {
	iniFile.mu.Lock()
	defer iniFile.mu.Unlock()
	iniFile.iniFile = monitorIniFile
}

//PrintDiff 打印出ini文件的修改差异
func (iniMonitor *IniFileMonitor) PrintDiff(oldName, newName string) {
	diffs, err := iniMonitor.iniFile.CompareIniDiff(oldName, newName)
	if err != nil {
		logFile.ErrorDoo(err)
	}
	var str string
	for _, diff := range diffs {
		switch diff.Operation {
		case doofile.Delete:
			str += "delete-" + "old:[" + diff.OldText + "]\n"
		case doofile.Add:
			str += "Add+" + "new:[" + diff.NewText + "]\n"
		case doofile.Modify:
			str += "modify^" + "old:[" + diff.OldText + "]" + "<===>" + "new:[" + diff.NewText + "]\n"
		}
	}

	if len(diffs) > 0 {
		logFile.InfoDoo("note====>>", oldName, "has modify\n", str)
	}
}

//GetChangeSection 获取当前ini文件被改动的section
func (iniMonitor *IniFileMonitor) GetChangeSection(oldName, newName string) map[string]bool {
	diffs, err := iniMonitor.iniFile.CompareIniDiff(oldName, newName)
	if err != nil {
		logFile.ErrorDoo(err)
	}

	m := make(map[string]bool)
	for _, diff := range diffs {
		m[diff.Section] = true
	}

	return m
}

// 元素去重
func RemoveRep(slc []string) []string {
	if len(slc) < 1024 {
		// 切片长度小于1024的时候，循环来过滤
		return RemoveRepByLoop(slc)
	} else {
		// 大于的时候，通过map来过滤
		return RemoveRepByMap(slc)
	}
}

// 通过两重循环过滤重复元素
func RemoveRepByLoop(slc []string) []string {
	result := []string{} // 存放结果
	for i := range slc {
		flag := true
		for j := range result {
			if slc[i] == result[j] {
				flag = false // 存在重复元素，标识为false
				break
			}
		}
		if flag { // 标识为false，不添加进结果
			result = append(result, slc[i])
		}
	}
	return result
}

// 通过map主键唯一的特性过滤重复元素
func RemoveRepByMap(slc []string) []string {
	result := []string{}
	tempMap := map[string]byte{} // 存放不重复主键
	for _, e := range slc {
		l := len(tempMap)
		tempMap[e] = 0
		if len(tempMap) != l { // 加入map后，map长度变化，则元素不重复
			result = append(result, e)
		}
	}
	return result
}
