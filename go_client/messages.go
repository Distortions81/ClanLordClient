package main

import "sync"

const maxMessages = 5

type MsgClass int

const (
        MsgDefault MsgClass = iota
        MsgInfo
        MsgShare
        MsgLogon
        MsgLogoff
        MsgError
        MsgMySpeech
)

type Message struct {
        Class MsgClass
        Text  string
}

var (
        messageMu sync.Mutex
        messages  []Message
)

func addMessage(class MsgClass, msg string) {
        messageMu.Lock()
        defer messageMu.Unlock()
        messages = append(messages, Message{Class: class, Text: msg})
        if len(messages) > maxMessages {
                messages = messages[len(messages)-maxMessages:]
        }
}

func getMessages() []Message {
        messageMu.Lock()
        defer messageMu.Unlock()
        out := make([]Message, len(messages))
        copy(out, messages)
        return out
}

