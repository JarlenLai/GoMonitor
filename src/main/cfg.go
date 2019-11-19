package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ini"
)

const (
	//文件类型切割符号
	FileTypeSplit string = ","
)

//MonitorCfg 监控程序的配置结构
type MonitorCfg struct {
	machineName     string            //当前监控的机器名
	serviceSpecName map[string]string //指定的service名字以及对应附件目录
	servicePartName []string          //监控的包含指定前缀的服务
	fileDir         map[string]string //需要监控的文件目录以及目录下的文件类型
	fileSpec        []string          //需要监控的指定文件
	logFileSize     int64             //日记文件大小
	emailData       EmailData
	refreshSCMTime  int
	mu              sync.RWMutex
}

//NewMonitorCfg New一个配置变量
func NewMonitorCfg() *MonitorCfg {
	return &MonitorCfg{serviceSpecName: make(map[string]string),
		servicePartName: make([]string, 0),
		fileDir:         make(map[string]string),
		fileSpec:        make([]string, 0),
		logFileSize:     800,
		emailData:       EmailData{receiveU: make([]string, 0)}}
}

//LoadAllCfg 加载配置文件中的所有配置
func (mcfg *MonitorCfg) LoadAllCfg(path string) error {
	strErr := ""
	if err := mcfg.LoadMonitorServiceCfg(path); err != nil {
		strErr += err.Error() + "\n"
	}

	if err := mcfg.LoadMonitorFileCfg(path); err != nil {
		strErr += err.Error() + "\n"
	}

	if err := mcfg.LoadMonitorComCfg(path); err != nil {
		strErr += err.Error() + "\n"
	}

	if strErr != "" {
		return fmt.Errorf("%s", strErr)
	} else {
		return nil
	}
}

//LoadMonitorServiceCfg 加载监控服务的配置
func (mcfg *MonitorCfg) LoadMonitorServiceCfg(path string) error {
	cfg, err := ini.Load(path)
	if err != nil {
		return err
	}

	mcfg.mu.Lock()
	defer mcfg.mu.Unlock()

	mcfg.serviceSpecName = make(map[string]string)
	mcfg.servicePartName = make([]string, 0)
	if sec, er := cfg.GetSection("MonitorServiceSpec"); er == nil {
		var beforeSuffix = -1
		keys := sec.Keys()
		for _, key := range keys {
			var suffix = 0
			if strings.HasPrefix(key.Name(), "Name") {
				suffix, _ = strconv.Atoi(strings.TrimPrefix(key.Name(), "Name"))
			} else if strings.HasPrefix(key.Name(), "Attach") {
				suffix, _ = strconv.Atoi(strings.TrimPrefix(key.Name(), "Attach"))
			} else {
				continue
			}

			if beforeSuffix == suffix {
				continue
			}
			beforeSuffix = suffix

			if sec.HasKey("Name" + strconv.Itoa(suffix)) {
				if sec.HasKey("Attach" + strconv.Itoa(suffix)) {
					mcfg.serviceSpecName[sec.Key("Name"+strconv.Itoa(suffix)).Value()] = sec.Key("Attach" + strconv.Itoa(suffix)).Value()
				} else {
					mcfg.serviceSpecName[sec.Key("Name"+strconv.Itoa(suffix)).Value()] = ""
				}
			}
		}
	}

	if sec, er := cfg.GetSection("MonitorServicePart"); er == nil {
		keys := sec.Keys()
		for _, key := range keys {
			var suffix = 0
			if strings.HasPrefix(key.Name(), "Name") {
				suffix, _ = strconv.Atoi(strings.TrimPrefix(key.Name(), "Name"))
			} else {
				continue
			}

			if sec.HasKey("Name" + strconv.Itoa(suffix)) {
				mcfg.servicePartName = append(mcfg.servicePartName, sec.Key("Name"+strconv.Itoa(suffix)).Value())
			}
		}
	}

	mcfg.refreshSCMTime = 300
	if sec, er := cfg.GetSection("MonitorServiceTimer"); er == nil {
		//定时刷新任务管理器中要监控的service的时间(主要针对新安装的并且需要监控的服务)
		if sec.HasKey("RefreshSCMTime") {
			mcfg.refreshSCMTime, _ = sec.Key("RefreshSCMTime").Int()
		}
	}

	return nil
}

//LoadMonitorFileCfg 加载监控文件的配置
func (mcfg *MonitorCfg) LoadMonitorFileCfg(path string) error {
	cfg, err := ini.Load(path)
	if err != nil {
		return fmt.Errorf("Fail to load file ini file:%s, err:%s", path, err.Error())
	}

	mcfg.mu.Lock()
	defer mcfg.mu.Unlock()

	//监控目录的配置
	mcfg.fileDir = make(map[string]string) //每次加载前先清除掉原来的
	sec, err := cfg.GetSection("MonitorFileDir")
	if err == nil {
		var count = 0
		keys := sec.Keys()
		for _, key := range keys {
			prefix := "Type"
			if strings.HasPrefix(key.Name(), "Path") {
				prefix = "Path"
			}
			suffix := strings.TrimPrefix(key.Name(), prefix)
			if j, _ := strconv.Atoi(suffix); count == j {
				continue
			}
			count++
			typen := "Type" + suffix
			pathn := "Path" + suffix
			if sec.HasKey(typen) && sec.HasKey(pathn) {
				keyT := sec.Key(typen)
				keyP := sec.Key(pathn)
				dirpath := keyP.Value()
				mcfg.fileDir[dirpath] = keyT.Value()
			}
		}
	}

	//指定监控文件的配置
	mcfg.fileSpec = make([]string, 0) //每次加载前先清除掉原来的
	sec, err = cfg.GetSection("MonitorFileSpec")
	if err == nil {
		keys := sec.Keys()
		for _, key := range keys {
			mcfg.fileSpec = append(mcfg.fileSpec, key.Value())
		}
	}

	return nil
}

//LoadMonitorComCfg 加载公共配置信息
func (mcfg *MonitorCfg) LoadMonitorComCfg(path string) error {
	cfg, err := ini.Load(path)
	if err != nil {
		return fmt.Errorf("Fail to load file ini file:%s, err:%s", path, err.Error())
	}

	mcfg.mu.Lock()
	defer mcfg.mu.Unlock()

	mcfg.machineName = "Unknow Machine Name"
	hasCfg := false
	if sec, er := cfg.GetSection("CommonData"); er == nil {

		//当前监控window机器的标识
		if sec.HasKey("MachineName") {
			mcfg.machineName = sec.Key("MachineName").Value()
		}

		//每个日记大小的配置
		if v, er := sec.GetKey("LogFileSize"); er == nil {
			if i, e := v.Int64(); e == nil {
				mcfg.logFileSize = i
				hasCfg = true
			}
		}
		if !hasCfg {
			mcfg.logFileSize = 0
		}
	}

	//邮件配置信息
	mcfg.emailData = EmailData{receiveU: make([]string, 0)}
	if sec, er := cfg.GetSection("CommonEmail"); er == nil {
		if sec.HasKey("Open") {
			mcfg.emailData.status, _ = sec.Key("Open").Int()
		}
		if sec.HasKey("Host") {
			mcfg.emailData.host = sec.Key("Host").Value()
		}
		if sec.HasKey("Port") {
			mcfg.emailData.port, _ = sec.Key("Port").Int()
		}
		if sec.HasKey("SendU") {
			mcfg.emailData.sendU = sec.Key("SendU").Value()
		}
		if sec.HasKey("SendP") {
			mcfg.emailData.sendP = sec.Key("SendP").Value()
		}
		if sec.HasKey("ReceiveU") {
			str := sec.Key("ReceiveU").Value()
			mcfg.emailData.receiveU = strings.Split(str, ",")

		}
	}

	return nil
}

//GetMonitorFileList 根据监控配置获取要监控的文件列表
func (mcfg *MonitorCfg) GetMonitorFileList() []string {
	files := make([]string, 0)
	mFiles := make(map[string]bool) //使用map类型去除重复元素
	filters := make([]string, 0)

	mcfg.mu.RLock()
	defer mcfg.mu.RUnlock()

	for k, v := range mcfg.fileDir {
		suffixs := strings.Split(v, FileTypeSplit)
		fs, err := GetAllFiles(k, suffixs)
		if err == nil {
			for _, f := range fs {
				mFiles[f] = true
			}
		}
	}

	for _, f := range mcfg.fileSpec {
		if strings.HasPrefix(f, "!") {
			f = strings.TrimPrefix(f, "!")
			filters = append(filters, f) //需要过滤的文件
			continue
		}
		mFiles[f] = true
	}

	//剔除需要过滤的文件
	for _, f := range filters {
		if _, ok := mFiles[f]; ok {
			delete(mFiles, f)
		}
	}

	//map转成slice元素返回
	for f, _ := range mFiles {
		files = append(files, f)
	}

	return files
}

//GetSpecServices 获取具体的监控服务名列表
func (mcfg *MonitorCfg) GetSpecServices() []string {
	mcfg.mu.RLock()
	defer mcfg.mu.RUnlock()
	services := make([]string, 0)
	for k := range mcfg.serviceSpecName {
		services = append(services, k)
	}
	return services
}

//GetPartServices 获取模糊的监控服务名列表
func (mcfg *MonitorCfg) GetPartServices() []string {
	mcfg.mu.RLock()
	defer mcfg.mu.RUnlock()
	services := mcfg.servicePartName
	return services
}

//GetEmailData 获取email的配置数据
func (mcfg *MonitorCfg) GetEmailData() *EmailData {
	mcfg.mu.Lock()
	ed := &EmailData{mcfg.emailData.status, mcfg.emailData.host, mcfg.emailData.port, mcfg.emailData.sendU, mcfg.emailData.sendP, mcfg.emailData.receiveU}
	mcfg.mu.Unlock()
	return ed
}

//GetMachineName 获取当前机器名
func (mcfg *MonitorCfg) GetMachineName() (name string) {
	mcfg.mu.Lock()
	name = mcfg.machineName
	mcfg.mu.Unlock()
	return name
}

//GetRefreshSCMTime 获取刷新任务管理器中service的时间(针对新安装服务及时加入监控)
func (mcfg *MonitorCfg) GetRefreshSCMTime() int {
	mcfg.mu.Lock()
	t := mcfg.refreshSCMTime
	mcfg.mu.Unlock()
	return t
}

//GetServiceAttachPath 获取当前服务的附件路径
func (mcfg *MonitorCfg) GetServiceAttachPath(service string) (string, bool) {
	mcfg.mu.RLock()
	defer mcfg.mu.RUnlock()
	if _, ok := mcfg.serviceSpecName[service]; ok {
		return mcfg.serviceSpecName[service], true
	}
	return "", false
}

//GetAllFiles 获取指定目录下的所有文件,包含子目录下的文件
func GetAllFiles(dirPth string, suffixs []string) (files []string, err error) {
	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		return nil, err
	}

	PthSep := string(os.PathSeparator)
	//suffix = strings.ToUpper(suffix) //忽略后缀匹配的大小写

	for _, fi := range dir {
		if fi.IsDir() { // 目录, 递归遍历
			if ls, err := GetAllFiles(dirPth+PthSep+fi.Name(), suffixs); err == nil {
				files = append(files, ls...)
			}
		} else {
			// 过滤指定格式
			for _, suffix := range suffixs {
				ok := strings.HasSuffix(fi.Name(), suffix)
				if ok {
					files = append(files, dirPth+PthSep+fi.Name())
					break
				}
			}
		}
	}

	return files, nil
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
