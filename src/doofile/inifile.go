package doofile

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ini"
)

const (
	Delete int8 = -1
	Add    int8 = 1
	Modify int8 = 0
)

type Diff struct {
	Operation int8
	Section   string
	OldText   string
	NewText   string
}

type IniFile struct {
	ini  map[string]*ini.File //ini文件路径以及对应的内容对象指针
	list []string             //当前的ini文件列表
	mu   sync.RWMutex
}

func NewIniFile() *IniFile {
	var iniFile = &IniFile{ini: make(map[string]*ini.File), list: make([]string, 0)}
	return iniFile
}

//LoadMonitorIniFiles 加载要监控的ini文件列表(返回值添加成功的列表，以及出错的信息)
func (file *IniFile) LoadMonitorIniFiles(pathList []string) ([]string, error) {
	strErr := ""
	iniFiles := make([]string, 0)
	file.mu.Lock()
	defer file.mu.Unlock()
	for _, path := range pathList {
		if IsIniFile(path) {
			cfg, err := ini.Load(path)
			if err != nil {
				strErr += "Fail to load file ini file:" + path + "err:" + err.Error() + "\r\n"
				continue
			}
			file.ini[path] = cfg
			iniFiles = append(iniFiles, path)
		}

	}

	file.list = pathList //赋值记录当前添加的最新的列表(不代表列表中的文件都添加成功)

	if strErr != "" {
		return iniFiles, fmt.Errorf("%s", strErr)
	}

	return iniFiles, nil
}

//LoadMonitorIniFile 加载要监控的ini文件
func (file *IniFile) LoadMonitorIniFile(path string) error {
	if !IsIniFile(path) {
		return fmt.Errorf("%s not is a ini file", path)
	}
	cfg, err := ini.Load(path)
	if err != nil {
		return fmt.Errorf("Fail to load file ini file:%s err:%s", path, err)
	}

	file.mu.Lock()
	defer file.mu.Unlock()
	file.ini[path] = cfg
	file.list = append(file.list, path)

	return nil
}

//UpdateMonitorIniFile 更新监控的iniFile信息(返回值添加成功的列表，以及出错的信息)
func (file *IniFile) UpdateMonitorIniFile(newList []string) ([]string, error) {
	file.mu.Lock()
	defer file.mu.Unlock()

	oldList := file.list
	delList := Difference(oldList, newList)
	addList := Difference(newList, oldList)

	//删除已经不需要的监控的ini文件信息
	for _, path := range delList {
		if _, ok := file.ini[path]; ok {
			delete(file.ini, path)
		}
	}

	//添加新的ini监控信息
	strErr := ""
	for _, path := range addList {
		if IsIniFile(path) {
			cfg, err := ini.Load(path)
			if err != nil {
				strErr += "Fail to load file ini file:" + path + "err:" + err.Error() + "\r\n"
				continue
			}
			file.ini[path] = cfg
		}
	}

	//当前存储监控的ini文件的信息
	iniFiles := make([]string, 0)
	for k, v := range file.ini {
		if v != nil {
			iniFiles = append(iniFiles, k)
		}
	}

	file.list = newList //重新赋值记录当前添加的最新的列表(不代表列表中的文件都添加成功)

	if strErr != "" {
		return iniFiles, fmt.Errorf("UpdateMonitorIniFile:\r\n%s", strErr)
	}

	return iniFiles, nil
}

func (file *IniFile) IsExistsMonitorIniFile(path string) bool {

	if !IsIniFile(path) {
		return true
	}

	file.mu.RLock()
	file.mu.RUnlock()
	_, ok := file.ini[path]
	return ok
}

func IsIniFile(path string) bool {
	if strings.HasSuffix(path, ".ini") {
		return true
	}
	return false
}

//CompareIniDiff 对比ini文件的差异，并返回差异结果
func (file *IniFile) CompareIniDiff(path1, path2 string) ([]Diff, error) {
	file.mu.RLock()
	file1, ok1 := file.ini[path1]
	if !ok1 || file1 == nil {
		file.mu.RUnlock()
		//file.ini not exists indicate path1 need to reload to file.ini
		err := file.LoadMonitorIniFile(path1)
		return nil, err
	}
	file.mu.RUnlock()

	file2, err := ini.Load(path2)
	if err != nil {
		//有些程序会频繁的修改文件，这里有打开文件失败的问题,因为文件被正在占用修改
		//logdoo.ErrorDoo("Fail to load file ini file:", path2, "err:", err)
		return nil, nil
	}

	diff := make([]Diff, 0)

	secDiff := CompareDiffSection(file1, file2)
	if len(secDiff) > 0 {
		diff = append(diff, secDiff...)
	}

	comSecs := GetIntersectSection(file1, file2)
	for _, v := range comSecs {
		sec1, _ := file1.GetSection(v)
		sec2, _ := file2.GetSection(v)
		keyDiff := CompareDiffKey(sec1, sec2)
		if len(keyDiff) > 0 {
			diff = append(diff, keyDiff...)
		}
	}

	file.mu.Lock()
	defer file.mu.Unlock()
	delete(file.ini, path1)
	file.ini[path2] = file2

	return diff, nil
}

//GetIntersectSection 获取两个文件内容的section交集结果
func GetIntersectSection(file1, file2 *ini.File) []string {
	name1 := file1.SectionStrings()
	name2 := file2.SectionStrings()

	unions := Intersect(name1, name2)

	return unions
}

//CompareDiffSection 比较ini文件添加或者删除来了哪些Section
func CompareDiffSection(file1, file2 *ini.File) []Diff {
	diff := make([]Diff, 0)
	name1 := file1.SectionStrings()
	name2 := file2.SectionStrings()

	differ1 := Difference(name1, name2)
	for _, v := range differ1 {
		sec, _ := file1.GetSection(v)
		keys := sec.Keys()
		var str string
		str += v + ":" + "\n"
		for _, v := range keys {
			str += v.Name() + "=" + v.Value() + "\n"
		}
		d := Diff{Delete, v, str, ""}
		diff = append(diff, d)
	}

	differ2 := Difference(name2, name1)
	for _, v := range differ2 {
		sec, _ := file2.GetSection(v)
		keys := sec.Keys()
		var str string
		str += v + ":" + "\n"
		for _, v := range keys {
			str += v.Name() + "=" + v.Value() + "\n"
		}
		d := Diff{Add, v, "", str}
		diff = append(diff, d)
	}

	return diff
}

//CompareDiffKey 对比ini文件个section下的key的改变
func CompareDiffKey(sec1, sec2 *ini.Section) []Diff {

	diff := make([]Diff, 0)

	var name1 = make([]string, 0)
	keys1 := sec1.Keys()
	for _, v := range keys1 {
		name1 = append(name1, v.Name())
	}

	var name2 = make([]string, 0)
	keys2 := sec2.Keys()
	for _, v := range keys2 {
		name2 = append(name2, v.Name())
	}

	diffks1 := Difference(name1, name2)
	for _, diffk1 := range diffks1 {
		key1 := sec1.Key(diffk1)
		d := Diff{Delete, sec1.Name(), sec1.Name() + ": " + diffk1 + "=" + key1.Value(), ""}
		diff = append(diff, d)
	}

	diffks2 := Difference(name2, name1)
	for _, diffk2 := range diffks2 {
		key2 := sec2.Key(diffk2)
		d := Diff{Add, sec2.Name(), sec2.Name(), sec2.Name() + ": " + diffk2 + "=" + key2.Value()}
		diff = append(diff, d)
	}

	intersects := Intersect(name2, name1)
	for _, intersect := range intersects {
		key1 := sec1.Key(intersect)
		key2 := sec2.Key(intersect)
		if key1.Value() != key2.Value() {
			d := Diff{Modify, sec2.Name(), sec1.Name() + ": " + intersect + "=" + key1.Value(), sec2.Name() + ": " + intersect + "=" + key2.Value()}
			diff = append(diff, d)
		}
	}

	return diff
}

//求并集
func Union(slice1, slice2 []string) []string {
	m := make(map[string]int)
	for _, v := range slice1 {
		m[v]++
	}

	for _, v := range slice2 {
		times, _ := m[v]
		if times == 0 {
			slice1 = append(slice1, v)
		}
	}
	return slice1
}

//求交集
func Intersect(slice1, slice2 []string) []string {
	m := make(map[string]int)
	nn := make([]string, 0)
	for _, v := range slice1 {
		m[v]++
	}

	for _, v := range slice2 {
		times, _ := m[v]
		if times == 1 {
			nn = append(nn, v)
		}
	}
	return nn
}

//求差集
func Difference(slice1, slice2 []string) []string {
	m := make(map[string]int)
	nn := make([]string, 0)
	inter := Intersect(slice1, slice2)
	for _, v := range inter {
		m[v]++
	}

	for _, value := range slice1 {
		times, _ := m[value]
		if times == 0 {
			nn = append(nn, value)
		}
	}
	return nn
}
