package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type Message struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}
type Client struct {
	process  *exec.Cmd
	input    io.WriteCloser
	messages chan Message
	done     chan error
	serial   int
	timeout  time.Duration
	forks    map[string][]Thread
}

func newClient(binary string, args []string, timeout time.Duration) (*Client, error) {
	cmd, err := command(binary, args)
	if err != nil {
		return nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	c := &Client{process: cmd, input: in, messages: make(chan Message, 64), done: make(chan error, 1), timeout: timeout}
	go func() {
		defer close(c.messages)
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 4096), 16*1024*1024)
		for scan.Scan() {
			var m Message
			if json.Unmarshal(scan.Bytes(), &m) == nil && m.ID != 0 {
				c.messages <- m
			}
		}
	}()
	go func() { c.done <- cmd.Wait() }()
	if err = c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "thread_cost", "version": version}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		c.Close()
		return nil, err
	}
	if err = c.send(map[string]string{"method": "initialized"}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Client) send(v any) error { return json.NewEncoder(c.input).Encode(v) }
func (c *Client) call(method string, params any, result any) error {
	c.serial++
	id := c.serial
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("Codex disconnected")
	}
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	for {
		select {
		case m, ok := <-c.messages:
			if !ok {
				return fmt.Errorf("Codex metadata server closed")
			}
			if m.ID != id {
				continue
			}
			if m.Error != nil {
				return fmt.Errorf("Codex rejected %s (code %d)", method, m.Error.Code)
			}
			if result != nil {
				return json.Unmarshal(m.Result, result)
			}
			return nil
		case <-timer.C:
			return fmt.Errorf("Codex timed out on %s", method)
		}
	}
}
func (c *Client) Read(id string) (Thread, error) {
	var r struct {
		Thread Thread `json:"thread"`
	}
	err := c.call("thread/read", map[string]any{"threadId": id, "includeTurns": false}, &r)
	if err != nil {
		return r.Thread, err
	}
	if c.forks == nil {
		c.forks, err = savedForks()
		if err != nil {
			return r.Thread, err
		}
	}
	if r.Thread.Parent == nil && r.Thread.Forked == nil {
		for parent, children := range c.forks {
			for _, child := range children {
				if child.ID == id {
					p := parent
					r.Thread.Forked = &p
				}
			}
		}
	}
	return r.Thread, err
}
func (c *Client) Rename(id, name string) error {
	return c.call("thread/name/set", map[string]string{"threadId": id, "name": name}, nil)
}
func (c *Client) Children(id string) ([]Thread, error) {
	if c.forks == nil {
		var err error
		c.forks, err = savedForks()
		if err != nil {
			return nil, err
		}
	}
	var children []Thread
	seen := map[string]bool{}
	for _, archived := range []bool{false, true} {
		cursor := ""
		cursors := map[string]bool{}
		for {
			params := map[string]any{"parentThreadId": id, "limit": 100, "archived": archived, "useStateDbOnly": true}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var page struct {
				Data []Thread `json:"data"`
				Next *string  `json:"nextCursor"`
			}
			if err := c.call("thread/list", params, &page); err != nil {
				return nil, err
			}
			for _, child := range page.Data {
				// Also detects older servers silently ignoring the parent filter.
				if parentID(child) != id {
					return nil, fmt.Errorf("Codex did not filter child chats")
				}
				if !seen[child.ID] {
					seen[child.ID] = true
					children = append(children, child)
				}
			}
			if page.Next == nil || *page.Next == "" {
				break
			}
			cursor = *page.Next
			if cursors[cursor] {
				return nil, fmt.Errorf("repeated child-list cursor")
			}
			cursors[cursor] = true
		}
	}
	for _, child := range c.forks[id] {
		if !seen[child.ID] {
			seen[child.ID] = true
			children = append(children, child)
		}
	}
	return children, nil
}
func (c *Client) Close() {
	c.input.Close()
	select {
	case <-c.done:
		return
	case <-time.After(500 * time.Millisecond):
	}
	_ = c.process.Process.Kill()
	select {
	case <-c.done:
	case <-time.After(time.Second):
	}
}
func connect(c Config) (*Client, error) {
	if c.Transport == "auto" || c.Transport == "proxy" {
		args := []string{"app-server", "proxy"}
		if c.Socket != "" {
			args = append(args, "--sock", c.Socket)
		}
		client, err := newClient(c.Binary, args, 2*time.Second)
		if err == nil || c.Transport == "proxy" {
			return client, err
		}
	}
	return newClient(c.Binary, []string{"app-server", "--listen", "stdio://"}, 5*time.Second)
}
