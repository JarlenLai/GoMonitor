package main

import (
	"fmt"
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
func (e *Email) SendEmailEx(subject, content, attach string) (error, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.status != EmailOpen {
		return nil, false
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.sendU)
	m.SetHeader("To", e.receiveU...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)
	m.Attach(attach)

	d := gomail.NewDialer(e.host, e.port, e.sendU, e.sendP)

	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("send eamilEx ex fail ==>> From:%s To:%s subject:%s content:%s attach:%s", e.sendU, e.receiveU, subject, content, attach), true
	}

	return nil, true
}

//SendEmail 发送邮件
func (e *Email) SendEmail(subject, content string) (error, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.status != EmailOpen {
		return nil, false
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.sendU)
	m.SetHeader("To", e.receiveU...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)

	d := gomail.NewDialer(e.host, e.port, e.sendU, e.sendP)

	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("send eamilEx ex fail ==>> From:%s To:%s subject:%s content:%s", e.sendU, e.receiveU, subject, content), true
	}

	return nil, true
}

func (e *Email) GetSendU() string {
	e.mu.RLock()
	str := e.sendU
	e.mu.RUnlock()
	return str
}

func (e *Email) GetReceiveU() []string {
	e.mu.RLock()
	list := e.receiveU
	e.mu.RUnlock()
	return list
}
