package shell

import (
	"bufio"
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	trysql "github.com/blainemoser/TrySql"
	"github.com/blainemoser/TrySql/utils"
	"github.com/blainemoser/trysqlshell/help"
)

var Testing bool
var TestHistoryOutput string

const timeFormat string = "15:04:05"
const shellVersion string = "1.0.0"

type TrySqlShell struct {
	TS           *trysql.TrySql
	OsInterrupt  chan os.Signal
	UserInput    chan string
	LastCaptured chan string
	ShellOutChan chan bool
	StdIn        io.Reader
	Reader       *bufio.Reader
	WG           *sync.WaitGroup
	Buffer       *list.List
}

type BufferObject struct {
	In     string
	Out    string
	Time   time.Time
	hidden bool
}

func New(ts *trysql.TrySql) *TrySqlShell {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	input := make(chan string)
	var stdIn io.Reader
	if Testing {
		var b []byte
		stdIn = bytes.NewReader(b)
	} else {
		stdIn = os.Stdin
	}
	reader := bufio.NewReader(stdIn)
	return &TrySqlShell{
		TS:           ts,
		OsInterrupt:  c,
		UserInput:    input,
		StdIn:        stdIn,
		Reader:       reader,
		WG:           &sync.WaitGroup{},
		Buffer:       list.New(),
		ShellOutChan: make(chan bool, 1),
		LastCaptured: make(chan string, 1),
	}
}

func (c *TrySqlShell) Start(test bool) {
	if test {
		go c.running()
		return
	}
	c.WG.Add(1)
	go c.running()
	c.WG.Wait()
}

func (c *TrySqlShell) greeting() {
	msg := fmt.Sprintf(" TrySql Shell version %s ", shellVersion)
	var line string
	for i := 0; i < len(msg); i++ {
		line += "_"
	}
	fmt.Print("\t" + line + "\n\n\t" + msg + "\n\t" + line + "\n\n")
}

func (c *TrySqlShell) running() {
	c.greeting()
	fmt.Println("shell ready")
	go c.waitForInput()
	for {
		select {
		case interruption := <-c.OsInterrupt:
			fmt.Println(" " + interruption.String())
			close(c.OsInterrupt)
			c.WG.Done()
			return
		case command := <-c.UserInput:
			c.capture(&command)
			if c.handleCommand(command) {
				return
			}
			go c.waitForInput()
		}
	}
}

func (c *TrySqlShell) handleCommand(command string) bool {
	if commandSplit, ok := c.checkHelp(command); ok {
		c.help(commandSplit)
		return false
	}
	if commandSplit, ok := c.checkQuery(command); ok {
		c.query(commandSplit)
		return false
	}
	if commandSplit, ok := c.checkDetails(command); ok {
		c.details(commandSplit)
		return false
	}
	switch command {
	case "":
		return false
	case "quit", "exit":
		return c.quit()
	case "container-details", "cd", "get-container-details":
		return c.containerDetails(command)
	case "container-id", "cid", "get-container-id":
		return c.containerID(command)
	case "password", "p", "get-password":
		return c.password(command)
	case "history", "hist", "hi":
		return c.getHistory()
	case "docker-version", "version", "dv":
		return c.getVersion(command)
	case "mysql", "ms":
		return c.mysql(command)
	case "[error]":
		return false
	default:
		c.waitForShellOutput(
			command,
			fmt.Sprintf(
				"> unrecognised command '%s'. Type 'help' for help",
				strings.ReplaceAll(command, "\n", ""),
			),
			false,
		)
	}

	return false
}

func (c *TrySqlShell) quit() bool {
	fmt.Println("> exiting...")
	close(c.UserInput)
	c.WG.Done()
	return true
}

func (c *TrySqlShell) help(command []string) {
	result := help.Get(command)
	c.waitForShellOutput("help", "\n"+result+"\n", true)
}

func (c *TrySqlShell) query(command []string) {
	if len(command) < 2 {
		return
	}
	query := c.getMultiCommand(command)
	result, err := c.TS.Query(query, true)
	if err != nil {
		result = err.Error()
	}
	c.waitForShellOutput(command[0]+" "+query, result, false)
}

func (c *TrySqlShell) details(command []string) {
	if len(command) < 1 {
		return
	}
	result := c.TS.GetDetails(command[1:])
	c.waitForShellOutput(strings.Join(command, " "), result, false)
}

func (c *TrySqlShell) getMultiCommand(command []string) string {
	command = command[1:]
	result := make([]string, len(command))
	for i := 0; i < len(command); i++ {
		result[i] = strings.Trim(command[i], "\"")
	}
	return strings.Join(result, " ")
}

func (c *TrySqlShell) checkHelp(command string) ([]string, bool) {
	return c.checkFirst(command, []string{"help", "h"})
}

func (c *TrySqlShell) checkQuery(command string) ([]string, bool) {
	return c.checkFirst(command, []string{"query", "q"})
}

func (c *TrySqlShell) checkDetails(command string) ([]string, bool) {
	return c.checkFirst(command, []string{"details", "d"})
}

func (c *TrySqlShell) checkFirst(command string, check []string) ([]string, bool) {
	split := strings.Split(command, " ")
	if len(split) < 1 {
		return []string{}, false
	}
	for _, v := range check {
		if split[0] == v {
			return split, true
		}
	}
	return []string{}, false
}

func (c *TrySqlShell) waitForShellOutput(input, msg string, hidden bool) {
	if Testing {
		c.shellOutput(input, msg, hidden)
		return
	}
	// if not in testing drain the channel
	<-c.shellOutput(input, msg, hidden)
}

func (c *TrySqlShell) shellOutput(input, msg string, hidden bool) chan bool {
	b := &BufferObject{
		In:     input,
		Out:    msg,
		Time:   time.Now(),
		hidden: hidden,
	}
	if c.Buffer.Len() >= c.TS.Configs.GetBufferSize() {
		e := c.Buffer.Back()
		c.Buffer.Remove(e)
	}
	c.Buffer.PushFront(b)
	fmt.Println(msg)
	c.ShellOutChan <- true
	return c.ShellOutChan
}

func (c *TrySqlShell) waitForInput() {
	fmt.Print("> ")
	userInput, err := c.Reader.ReadString('\n')
	if err != nil && userInput == "\n" {
		err = errors.New("carriage_return")
	}
	if err != nil {
		c.bufferError(err)
		return
	}
	c.special(&userInput)
	if userInput == "" {
		c.UserInput <- "[error]"
	}
	userInput = strings.TrimSuffix(userInput, "\n")
	c.UserInput <- userInput
}

func (c *TrySqlShell) bufferError(err error) {
	switch err.Error() {
	case "EOF":
		return
	case "carriage_return":
		c.UserInput <- "[error]"
		return
	default:
		c.waitForShellOutput("[error]", "> An error occured ("+err.Error()+"), please try again", true)
	}
}

func (c *TrySqlShell) Push(input string) {
	c.UserInput <- input
	<-c.LastCaptured
}

func (c *TrySqlShell) getHistory() bool {
	fmt.Println()
	var message string
	for e := c.Buffer.Front(); e != nil; e = e.Next() {
		if e.Value != nil {
			if add, ok := e.Value.(*BufferObject); ok {
				if add.hidden {
					continue
				}
				message += add.getMessage()
			}
		}
	}
	if len(message) > 0 {
		fmt.Println(message)
	}
	if Testing {
		TestHistoryOutput = message
	}
	return false
}

func (b *BufferObject) getMessage() string {
	message := ""
	if len(b.In) > 0 {
		message += "\t" + b.In + "\n"
	}
	if len(b.Out) > 0 {
		outMsg := strings.Replace(b.Out, "> ", "", 1)
		utils.TruncString(&outMsg, 100)
		message += "\t-> " + outMsg + "\n\t   at " + b.Time.Format(timeFormat) + "\n\n"
	} else {
		message += "\t->\n\t   at " + b.Time.Format(timeFormat) + "\n\n"
	}
	return message
}

func (c *TrySqlShell) containerDetails(command string) bool {
	c.waitForShellOutput(command, "> "+c.TS.GetContainerDetails(false), false)
	return false
}

func (c *TrySqlShell) containerID(command string) bool {
	c.waitForShellOutput(command, "> "+c.TS.GetContainerDetails(true), false)
	return false
}

func (c *TrySqlShell) password(command string) bool {
	c.waitForShellOutput(command, "> "+c.TS.Password(), false)
	return false
}

func (c *TrySqlShell) LastOutput() string {
	e := c.Buffer.Front()
	if e == nil {
		return ""
	}
	if e.Value == nil {
		return ""
	}
	if lastBuffer, ok := e.Value.(*BufferObject); ok {
		return lastBuffer.Out
	}
	return ""
}

func (c *TrySqlShell) getVersion(command string) bool {
	c.waitForShellOutput(command, "> "+c.TS.DockerVersion(), false)
	return false
}

func (c *TrySqlShell) mysql(command string) bool {
	c.waitForShellOutput(command, "> "+c.TS.MySQLCommand(), false)
	return false
}

func (c *TrySqlShell) capture(command *string) {
	c.sanitize(command)
	if len(c.LastCaptured) > 0 {
		// Drain channel
		<-c.LastCaptured
	}
	c.LastCaptured <- *command
}

func (c *TrySqlShell) lastCommand() string {
	for e := c.Buffer.Front(); e != nil; e = e.Next() {
		if e.Value != nil {
			if add, ok := e.Value.(*BufferObject); ok {
				if len(add.In) < 0 {
					continue
				}
				return add.In
			}
		}
	}
	return ""
}

func (c *TrySqlShell) sanitize(command *string) {
	*command = strings.ReplaceAll(*command, "\n", "")
	*command = strings.ReplaceAll(*command, "\t", "")
	*command = strings.Trim(*command, " ")
}

func (c *TrySqlShell) special(userInput *string) {
	bytes := []byte(*userInput)
	if len(bytes) >= 3 {
		if bytes[0] == 27 && bytes[1] == 91 {
			if bytes[2] == 65 {
				*userInput = c.lastCommand()
				return
			}
			*userInput = ""
		}
	}
}
