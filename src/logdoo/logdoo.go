package logdoo

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int32

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

/*
===================
 utils functions
===================
*/
func fileSize(file string) int64 {
	f, e := os.Stat(file)
	if e != nil {
		fmt.Println(e.Error())
		return 0
	}
	return f.Size()
}

func isExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil || os.IsExist(err)
}

func CreateLogDir(path string) error {
	if isExist(path) {
		return nil
	}

	err := os.Mkdir(path, os.ModePerm)
	if err != nil {
		return fmt.Errorf("os.Mkdir err:%s", err.Error())
	}

	return nil
}

/*
===================
 log handlers
===================
*/
type Handler interface {
	SetOutput(w io.Writer)
	Output(calldepth int, s string) error

	DebugDoo(v ...interface{})
	InfoDoo(v ...interface{})
	WarnDoo(v ...interface{})
	ErrorDoo(v ...interface{})

	Flags() int
	SetFlags(flag int)
	Prefix() string
	SetPrefix(prefix string)
	close()
}

type LogHandler struct {
	lg *Logger
}

type ConsoleHander struct {
	LogHandler
}

type RotatingHandler struct {
	LogHandler
	dir      string
	filename string
	maxNum   int
	maxSize  int64
	suffix   int
	logfile  *os.File
	mu       sync.Mutex
}

//NewConsoleHandler New一个控制台日记变量
func NewConsoleHandler() *ConsoleHander {
	l := New(os.Stderr, "", Ltime|Lmicroseconds|Lshortfile)
	return &ConsoleHander{LogHandler: LogHandler{l}}
}

func NewRotatingHandler(dir string, filename string, maxNum int, maxSize int64) *RotatingHandler {
	logfile, _ := os.OpenFile(dir+"/"+filename, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
	l := New(logfile, "", LstdFlags)

	h := &RotatingHandler{
		LogHandler: LogHandler{l},
		dir:        dir,
		filename:   filename,
		maxNum:     maxNum,
		maxSize:    maxSize,
		suffix:     0,
	}

	if h.isMustRename() {
		h.rename()
	} else {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.lg.SetOutput(logfile)
	}

	// monitor filesize per second
	go func() {
		timer := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-timer.C:
				h.fileCheck()
			}
		}
	}()

	return h
}

//NewDayLogHandle 创建每天日记文件
func NewDayLogHandle(dir string, maxSize int64) *RotatingHandler {
	h := &RotatingHandler{
		LogHandler: LogHandler{nil},
		dir:        dir,
		maxNum:     1,
		maxSize:    maxSize * 1024 * 1024,
		suffix:     0,
	}

	return h
}

func (h *RotatingHandler) SetDayLogHandleDir(path string) {
	h.mu.Lock()
	h.dir = path
	h.mu.Unlock()
}

func (h *RotatingHandler) SetDayLogHandleFileSize(size int64) {
	if size <= 0 {
		return
	}

	h.mu.Lock()
	h.maxSize = size * 1024 * 1024
	h.mu.Unlock()
}

/*
===================
 LogHandler method
===================
*/
func (l *LogHandler) SetOutput(w io.Writer) {
	l.lg.SetOutput(w)
}

func (l *LogHandler) Output(calldepth int, s string) error {
	return l.lg.Output(calldepth, s)
}

func (l *LogHandler) Printf(format string, v ...interface{}) {
	l.lg.Printf(format, v...)
}

func (l *LogHandler) Print(v ...interface{}) { l.lg.Print(v...) }

func (l *LogHandler) Println(v ...interface{}) { l.lg.Println(v...) }

func (l *LogHandler) Fatal(v ...interface{}) {
	l.lg.Output(2, fmt.Sprint(v...))
}

func (l *LogHandler) Fatalf(format string, v ...interface{}) {
	l.lg.Output(2, fmt.Sprintf(format, v...))
}

func (l *LogHandler) Fatalln(v ...interface{}) {
	l.lg.Output(2, fmt.Sprintln(v...))
}

func (l *LogHandler) Flags() int {
	return l.lg.Flags()
}

func (l *LogHandler) SetFlags(flag int) {
	l.lg.SetFlags(flag)
}

func (l *LogHandler) Prefix() string {
	return l.lg.Prefix()
}

func (l *LogHandler) SetPrefix(prefix string) {
	l.lg.SetPrefix(prefix)
}

func (l *LogHandler) DebugDoo(v ...interface{}) {
	l.lg.Output(3, fmt.Sprintln("debug", v, "\r\n"))
}

func (l *LogHandler) InfoDoo(v ...interface{}) {
	l.lg.OutputNoCallDep(fmt.Sprintln("info", v, "\r\n"))
}

func (l *LogHandler) WarnDoo(v ...interface{}) {
	l.lg.Output(3, fmt.Sprintln("warn", v, "\r\n"))
}

func (l *LogHandler) ErrorDoo(v ...interface{}) {
	l.lg.Output(3, fmt.Sprintln("error", v, "\r\n"))
}

//RenameDoo 操作当前日记文件的名字
func (h *RotatingHandler) RenameDoo() {

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.maxSize <= 0 {
		return
	}

	now := time.Now() // get this early.
	year, month, day := now.Date()
	fileName := fmt.Sprintf("%d%02d%02d.log", year, month, day)

	//是当天的分割文件
	if strings.Contains(h.filename, "_") && isExist(fileName) {
		fileName = h.filename
	}

	//文件超过最大限制新建一个后缀文件
	if isExist(h.dir+"/"+fileName) && fileSize(h.dir+"/"+fileName) >= h.maxSize {

	LOOP: //如果对应的后缀文件已经存在就继续累计后缀
		if isExist(h.dir+"/"+fileName) && fileSize(h.dir+"/"+fileName) >= h.maxSize {
			h.suffix++
			fileName = fmt.Sprintf("%d%02d%02d_%d.log", year, month, day, h.suffix)
			goto LOOP
		}

		logfile, _ := os.OpenFile(h.dir+"/"+fileName, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
		l := New(logfile, "", Ltime|Lmicroseconds|Lshortfile)
		h.lg = l
		h.filename = fileName
		if h.logfile != nil {
			h.logfile.Close()
			h.logfile = logfile
		}
		h.lg.SetOutput(logfile)
		return
	}

	//每天一个文件
	if !isExist(h.dir+"/"+fileName) || h.lg == nil {
		logfile, _ := os.OpenFile(h.dir+"/"+fileName, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
		l := New(logfile, "", Ltime|Lmicroseconds|Lshortfile)
		h.lg = l
		h.filename = fileName
		if h.logfile != nil {
			h.logfile.Close()
			h.logfile = logfile
		}
		h.lg.SetOutput(logfile)
		return
	}

}

func (l *LogHandler) close() {

}

func (h *RotatingHandler) close() {
	if h.logfile != nil {
		h.logfile.Close()
	}
}

func (h *RotatingHandler) isMustRename() bool {
	if h.maxNum > 1 {
		if fileSize(h.dir+"/"+h.filename) >= h.maxSize {
			return true
		}
	}
	return false
}

func (h *RotatingHandler) rename() {
	h.suffix = h.suffix%h.maxNum + 1

	if h.logfile != nil {
		h.logfile.Close()
	}

	newpath := fmt.Sprintf("%s/%s.%d.log", h.dir, h.filename, h.suffix)
	if isExist(newpath) {
		os.Remove(newpath)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	filepath := h.dir + "/" + h.filename
	os.Rename(filepath, newpath)
	h.logfile, _ = os.Create(filepath)
	h.lg.SetOutput(h.logfile)
}

func (h *RotatingHandler) fileCheck() {
	defer func() {
		if err := recover(); err != nil {
			Println(err)
		}
	}()
	if h.isMustRename() {
		h.rename()
	}
}

/*
===================
 logger
===================
*/
type _Logger struct {
	handlers []Handler
	level    Level
	mu       sync.Mutex
}

func NewLogger() *_Logger {
	return &_Logger{
		handlers: []Handler{},
		level:    DEBUG,
	}
}

//SetHandlers 设置打印日记变量
func (logger *_Logger) SetHandlers(handlers ...Handler) {
	logger.mu.Lock()
	logger.handlers = handlers
	logger.mu.Unlock()
}

func (logger *_Logger) SetLevel(level Level) {
	logger.mu.Lock()
	logger.level = level
	logger.mu.Unlock()
}

func (logger *_Logger) DebugDoo(v ...interface{}) {
	if logger.level <= DEBUG {
		for i := range logger.handlers {
			if t, ok := logger.handlers[i].(*RotatingHandler); ok {
				t.RenameDoo()
			}
			logger.handlers[i].DebugDoo(v...)
		}
	}
}

func (logger *_Logger) InfoDoo(v ...interface{}) {
	if logger.level <= INFO {
		for i := range logger.handlers {
			if t, ok := logger.handlers[i].(*RotatingHandler); ok {
				t.RenameDoo()
			}
			logger.handlers[i].InfoDoo(v...)
		}
	}
}

func (logger *_Logger) WarnDoo(v ...interface{}) {
	if logger.level <= WARN {
		for i := range logger.handlers {
			if t, ok := logger.handlers[i].(*RotatingHandler); ok {
				t.RenameDoo()
			}
			logger.handlers[i].WarnDoo(v...)
		}
	}
}

func (logger *_Logger) ErrorDoo(v ...interface{}) {
	if logger.level <= ERROR {
		for i := range logger.handlers {
			if t, ok := logger.handlers[i].(*RotatingHandler); ok {
				t.RenameDoo()
			}
			logger.handlers[i].ErrorDoo(v...)
		}
	}
}

func (logger *_Logger) Close() {
	for i := range logger.handlers {
		logger.handlers[i].close()
	}
}

func (logger *_Logger) SetDayLogHandleDir(path string) {
	for i := range logger.handlers {
		if t, ok := logger.handlers[i].(*RotatingHandler); ok {
			t.SetDayLogHandleDir(path)
		}
	}
}

func (logger *_Logger) SetDayLogHandleFileSize(size int64) {
	for i := range logger.handlers {
		if t, ok := logger.handlers[i].(*RotatingHandler); ok {
			t.SetDayLogHandleFileSize(size)
		}
	}
}
