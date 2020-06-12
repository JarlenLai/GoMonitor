package main

import (
	"GoMonitor/logdoo"
	"sync"

	"gopkg.in/gomail.v2"
)

const (
	EmailClose = 0
	EmailOpen  = 1
)

type EmailData struct {
	status   int
	host     string
	port     int
	sendU    string
	sendP    string
	receiveU []string
}

type Email struct {
	EmailData
	mu sync.RWMutex
}

//NewEmail New邮件实例
func NewEmail() *Email {
	return &Email{EmailData: EmailData{status: EmailClose,
		host:     "127.0.0.1",
		port:     25,
		sendU:    "sendU",
		sendP:    "sendP",
		receiveU: make([]string, 0)}}
}

//UpdateEmail 更新邮件配置信息
func (e *Email) UpdateEmail(ed *EmailData) {
	e.mu.Lock()
	e.status = ed.status
	e.host = ed.host
	e.port = ed.port
	e.sendU = ed.sendU
	e.sendP = ed.sendP
	e.receiveU = ed.receiveU
	e.mu.Unlock()
}

//SendEmailEx 发送邮件并且附带附件
func (e *Email) SendEmailEx(subject, content, attach string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.status != EmailOpen {
		return
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.sendU)
	m.SetHeader("To", e.receiveU...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)
	m.Attach(attach)

	d := gomail.NewDialer(e.host, e.port, e.sendU, e.sendP)

	if err := d.DialAndSend(m); err != nil {
		logdoo.InfoDoo("send eamilEx ex fail ==>> From:", e.sendU, "To:", e.receiveU, "subject:", subject, "content:", content, "attach:", attach)
	} else {
		logdoo.InfoDoo("send eamilEx ex success ==>> From:", e.sendU, "To:", e.receiveU, "subject:", subject, "content:", content, "attach:", attach)
	}
}

//SendEmail 发送邮件
func (e *Email) SendEmail(subject, content string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.status != EmailOpen {
		return
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.sendU)
	m.SetHeader("To", e.receiveU...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)

	d := gomail.NewDialer(e.host, e.port, e.sendU, e.sendP)

	if err := d.DialAndSend(m); err != nil {
		logdoo.InfoDoo("send eamil ex fail ==>> From:", e.sendU, "To:", e.receiveU, "subject:", subject, "content:", content)
	} else {
		logdoo.InfoDoo("send eamil ex success ==>> From:", e.sendU, "To:", e.receiveU, "subject:", subject, "content:", content)
	}
}
