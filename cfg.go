package main

import (
	"strconv"
	"strings"
	"sync"

	"gopkg.in/ini.v1"
)

//MonitorCfg 监控程序的配置结构
type MonitorCfg struct {
	machineName     string            //当前监控的机器名
	serviceSpecName map[string]string //指定的service名字以及对应附件目录
	servicePartName []string          //监控的包含指定前缀的服务
	emailData       EmailData
	refreshTime     int
	mu              sync.RWMutex
}

//NewMonitorCfg New一个配置变量
func NewMonitorCfg() *MonitorCfg {
	return &MonitorCfg{serviceSpecName: make(map[string]string),
		servicePartName: make([]string, 0),
		emailData:       EmailData{receiveU: make([]string, 0)}}
}

//LoadCfg 加载配置文件
func (mcfg *MonitorCfg) LoadCfg(path string) error {
	cfg, err := ini.Load(path)
	if err != nil {
		return err
	}

	mcfg.mu.Lock()
	defer mcfg.mu.Unlock()

	mcfg.machineName = "Unknow Machine Name"
	if sec, er := cfg.GetSection("Machine"); er == nil {
		if sec.HasKey("Name") {
			mcfg.machineName = sec.Key("Name").Value()
		}
	}

	mcfg.serviceSpecName = make(map[string]string)
	mcfg.servicePartName = make([]string, 0)
	if sec, er := cfg.GetSection("SpecInfo"); er == nil {
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

	if sec, er := cfg.GetSection("PartInfo"); er == nil {
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

	mcfg.emailData = EmailData{receiveU: make([]string, 0)}
	if sec, er := cfg.GetSection("EmailInfo"); er == nil {
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

	mcfg.refreshTime = 300
	if sec, er := cfg.GetSection("Timer"); er == nil {
		if sec.HasKey("RefreshCfg") {
			mcfg.refreshTime, _ = sec.Key("RefreshCfg").Int()
		}
	}

	return nil
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

//GetRefreshCfgTime 获取刷新时间
func (mcfg *MonitorCfg) GetRefreshTime() int {
	mcfg.mu.Lock()
	t := mcfg.refreshTime
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
