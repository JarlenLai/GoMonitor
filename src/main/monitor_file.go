package main

import (
	"doofile"
	"fmt"
	"sync"
	"time"

	"github.com/fsnotify"

)

//MonitorFile 监控的文件列表
type MonitorFile struct {
	watcher  *fsnotify.Watcher //监控文件对象
	fileMap  map[string]bool   //监控的文件路径以及该文件是有已经成功加入监控路径
	fileList []string          //当前要监控的文件列表,从cfg文件中解析过来的
	stopChan chan bool         //停止监控功能通知
	iniFile  IniFileMonitor    //ini 文件监控对象
	mu       sync.RWMutex
}

//IniFileMonitor 监控ini文件
type IniFileMonitor struct {
	iniFile *doofile.IniFile
	mu      sync.Mutex
}

//NewMonitorFile New一个监控文件列表存储结构变量
func NewMonitorFile() *MonitorFile {
	m := &MonitorFile{watcher: nil, fileMap: make(map[string]bool), fileList: make([]string, 0), stopChan: make(chan bool)}
	return m
}

//NewIniFileMonitor New一个监控ini文件的存储结构信息
func NewIniFileMonitor() *IniFileMonitor {
	iniFile := &IniFileMonitor{}
	return iniFile
}

//UpdateMonitorServiceCfg 配置文件修改时更新同时更新监控数据
func UpdateMonitorFileCfg(mc *MonitorCfg, mf *MonitorFile, e *Email, cfgPath string) error {
	if err := mc.LoadMonitorFileCfg(cfgPath); err != nil {
		return fmt.Errorf("LoadMonitorFileCfg err:%s", err)
	}

	paths := monitorCfg.GetMonitorFileList() //获取监控文件列表
	mf.UpdateWatcherFile(paths)              //更新监控文件列表(新添加的监控文件需要加入)
	mf.RefreshWatcherFile()                  //刷新监控文件(监控失败的需要从新监控)
	files := mf.GetMonitorFiles()

	var str string
	for _, f := range files {
		str += f + "\n"
	}
	logFile.InfoDoo(fmt.Sprintf("UpdateMonitorFileCfg cur monitor files:%d\r\n%s", len(files), str))

	return nil
}

//UpdateWatcherFile 更新监控文件
func (files *MonitorFile) UpdateWatcherFile(paths []string) {
	files.mu.Lock()
	defer files.mu.Unlock()

	//先更新加载监控的ini对象
	files.iniFile.mu.Lock()
	if files.iniFile.iniFile != nil {
		_, err := files.iniFile.iniFile.UpdateMonitorIniFile(paths)
		if err != nil {
			logFile.WarnDoo("UpdateWatcherFile:", err)
		}
	}
	files.iniFile.mu.Unlock()

	//删除不需要的文件监控
	delPath := doofile.Difference(files.fileList, paths)
	for _, v := range delPath {
		if isWatch, ok := files.fileMap[v]; ok {
			if isWatch {
				files.watcher.Remove(v)
			}
			delete(files.fileMap, v)
		}
	}

	//添加新的文件监控
	addPath := doofile.Difference(paths, files.fileList)
	for _, v := range addPath {
		files.fileMap[v] = false
		if PathExists(v) {
			err := files.watcher.Add(v)
			if err != nil {
				logFile.ErrorDoo("add watch file:", v, "err:", err)
				continue
			}
			files.fileMap[v] = true
		}
	}

	files.fileList = paths
}

//RefreshWatcherFile 刷新监控文件map,对监控失败或者没有进行监控的进行监控尝试
func (files *MonitorFile) RefreshWatcherFile() {
	files.mu.Lock()
	for k, v := range files.fileMap {
		if !v && PathExists(k) {
			err := files.watcher.Add(k)
			if err != nil {
				logFile.ErrorDoo("RefreshWatcherFile add watch file:", k, "err:", err)
				files.fileMap[k] = false
			}
			files.fileMap[k] = true
		} else if v && !PathExists(k) {
			files.fileMap[k] = false
		}
	}
	files.mu.Unlock()
}

//UpdateFileWatchFlag 更新监控文件的标志
func (files *MonitorFile) UpdateFileWatchFlag(path string, f bool) {
	files.mu.Lock()
	if _, ok := files.fileMap[path]; ok {
		files.fileMap[path] = f
	} else {
		logFile.ErrorDoo("UpdateFileWatchFlag err:", path, "is not exist:")
	}
	files.mu.Unlock()
}

//FilejoinWatcher 文件加入监控
func (files *MonitorFile) FilejoinWatcher(path string) {
	files.mu.Lock()
	if v, ok := files.fileMap[path]; ok {
		if !v {
			err := files.watcher.Add(path)
			if err != nil {
				logFile.ErrorDoo("FilejoinWatcher add watch file:", path, "err:", err)
			}
			files.fileMap[path] = true
		}
	} else {
		logFile.InfoDoo("FilejoinWatcher :", path, "is not exist:")
		files.fileMap[path] = false
	}
	files.mu.Unlock()
}

//StartWatcher 开始进行监控
func (files *MonitorFile) StartWatcher(paths []string, cfg *MonitorCfg) {

	files.mu.Lock()
	files.fileList = paths
	iniMonitor := doofile.NewIniFile()
	if _, err := iniMonitor.LoadMonitorIniFiles(paths); err != nil {
		logFile.InfoDoo(err)
	}
	files.iniFile.SetIniFileMonitor(iniMonitor)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logFile.InfoDoo("fsnotify.NewWatcher err:", err)
	}
	files.watcher = watcher //后面要记得释放资源(目前在Release中释放)
	files.mu.Unlock()

	go func() {
		var oldName string
		var isRename = false
		for {
			select {
			case event, ok := <-files.watcher.Events:
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
						files.iniFile.PrintDiff(oldName, event.Name)
					} else {
						if len([]rune(event.Name)) > 0 {
							logFile.InfoDoo("monitor event:", event)
						}

					}

				} else if event.Op&fsnotify.Remove == fsnotify.Remove {
					if len([]rune(event.Name)) > 0 {
						logFile.InfoDoo("monitor event:", event)
						files.UpdateFileWatchFlag(event.Name, false) //文件被移除需要更新文件监控标志

						//获取ini文件恢复标志
						cfg.mu.RLock()
						recoverFlag := cfg.removeRecover
						cfg.mu.RUnlock()

						//对于被删除的ini文件进行恢复
						if recoverFlag == 1 && doofile.IsIniFile(event.Name) {
							if err := files.iniFile.iniFile.RecoverFile(event.Name); err == nil {
								files.FilejoinWatcher(event.Name) //恢复成功的文件重新加入监控中,这个有时候内部的watcher.Add会导致阻塞
								logFile.InfoDoo("Remove file:", event.Name, "recover Ok")
							} else {
								logFile.InfoDoo("Remove file:", event.Name, "recover err:", err)
							}
						} else {
							logFile.InfoDoo("Remove file Op end")
						}
					}
				} else {
					if len([]rune(event.Name)) > 0 {
						logFile.InfoDoo("monitor event:", event)
					}
				}

			case _, ok := <-files.watcher.Errors:
				if !ok {
					return
				}
				//这里有个bug https://github.com/fsnotify/fsnotify/issues/72
				//logFile.ErrorDoo("monitor event:", err)

			case <-files.stopChan:
				return
			}
		}
	}()

	//添加监控文件通知
	files.mu.Lock()
	iniFiles := make([]string, 0)
	for _, path := range files.fileList {
		err = files.watcher.Add(path)
		if err != nil {
			logFile.ErrorDoo("add watch file:", path, "err:", err)
			files.fileMap[path] = false
			continue
		}
		iniFiles = append(iniFiles, path)
		files.fileMap[path] = true
	}
	files.mu.Unlock()

	var str string
	for _, iniFile := range iniFiles {
		str += iniFile + "\n"
	}
	logFile.InfoDoo(fmt.Sprintf("cur monifile file list:%d\r\n%s", len(iniFiles), str))

	//定时检查并对添加监控失败的文件进行重新监控
	go func() {
		timer := time.NewTicker(60 * time.Second)
		for {
			select {
			case <-timer.C:
				files.RefreshWatcherFile()
			}
		}
	}()

}

//Release 释放资源
func (files *MonitorFile) Release() {
	if files.watcher != nil {
		files.watcher.Close()
	}
}

//GetMonitorFiles 获取当前的监控文件列表
func (files *MonitorFile) GetMonitorFiles() []string {
	list := make([]string, 0)
	files.mu.Lock()
	for k, v := range files.fileMap {
		if v {
			list = append(list, k)
		}
	}
	files.mu.Unlock()

	return list
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
