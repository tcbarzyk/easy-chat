package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

type BroadcastType int

const port = "9000"

const (
	ToAll BroadcastType = iota
	ToSender
	ToAllButSender
	ToUser
)

type Client struct {
	conn         net.Conn
	username     string
	messageCount int
	writeChan    chan string
}

type Message struct {
	sender        *Client
	recipient     *Client
	content       string
	broadcastType BroadcastType
}

type LeaveEvent struct {
	client *Client
}

type UserListRequest struct {
	reply chan []string
}

type RegisterRequest struct {
	client   *Client
	username string
	reply    chan bool
}

func NewClient(conn net.Conn) *Client {
	c := &Client{
		conn:         conn,
		messageCount: 0,
		writeChan:    make(chan string, 10),
	}
	go c.writeLoop()
	return c
}

func (client *Client) writeLoop() {
	defer client.conn.Close()
	for msg := range client.writeChan {
		_, err := fmt.Fprintln(client.conn, msg)
		if err != nil {
			return
		}
		//fmt.Fprint(client.conn, "> ")
	}
}

var clients = make(map[net.Conn]*Client)
var msgChan = make(chan Message)
var leaveChan = make(chan LeaveEvent)
var userListChan = make(chan UserListRequest)
var registerChan = make(chan RegisterRequest)

func main() {
	portStr := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", portStr)
	if err != nil {
		log.Fatal("Error listening:", err)
	}

	defer listener.Close()

	log.Printf("Server listening on port %s...\n", port)
	go hub()

	for {

		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accepting conn:", err)
			continue
		}

		go handleConnection(conn)
	}
}

func hub() {
	for {
		select {
		case msg := <-msgChan:
			broadcast(msg)
		case event := <-leaveChan:
			delete(clients, event.client.conn)
			close(event.client.writeChan)
		case req := <-userListChan:
			userList := getConnectedUsers()
			req.reply <- userList
		case req := <-registerChan:
			exists := usernameExists(req.username)
			if !exists {
				req.client.username = req.username
				clients[req.client.conn] = req.client
				req.reply <- true
			} else {
				req.reply <- false
			}
		}
	}
}

func broadcast(msg Message) {
	switch msg.broadcastType {
	case ToAll:
		for _, client := range clients {
			client.writeChan <- msg.content
		}
	case ToSender:
		msg.sender.writeChan <- msg.content
	case ToAllButSender:
		for _, client := range clients {
			if client.conn != msg.sender.conn {
				client.writeChan <- msg.content
			}
		}
	case ToUser:
		if msg.recipient != nil {
			msg.recipient.writeChan <- msg.content
		}
	}
}

func getConnectedUsers() []string {
	usernames := make([]string, 0)
	for _, c := range clients {
		if c.username != "" {
			usernames = append(usernames, c.username)
		}
	}
	return usernames
}

func formatConnectedUsers() string {
	var b strings.Builder

	reply := make(chan []string)
	userListChan <- UserListRequest{reply: reply}
	users := <-reply

	fmt.Fprintf(&b, "There are currently %d connected users\n", len(users))
	for _, username := range users {
		fmt.Fprintf(&b, "- %s\n", username)
	}
	return strings.TrimSpace(b.String())
}

func usernameExists(username string) bool {
	for _, c := range clients {
		if username == c.username {
			return true
		}
	}
	return false
}

func handleConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)

	client := NewClient(conn)

	msg := "Welcome to the chatroom."
	sendToClient(client, msg)

	sendToClient(client, formatConnectedUsers())

	err := registerUser(client, reader)
	if err != nil {
		return
	}

	defer func() {
		leaveChan <- LeaveEvent{client: client}
	}()

	msg = fmt.Sprintf("You have joined the chatroom as %s.", client.username)
	sendToClient(client, msg)

	msg = fmt.Sprintf("%s has joined the chat.", client.username)
	broadcastFrom(client, msg)

	for {
		//fmt.Fprint(conn, "> ")
		message, err := receiveMessage(reader)
		if err != nil {
			msg := fmt.Sprintf("%s has left the chat.", client.username)
			sendToClient(client, msg)
			return
		} else if message == "" {
			continue
		} else if strings.HasPrefix(message, "/") {
			cmd := handleCommand(client, strings.ToLower(message[1:]))
			if cmd == "quit" {
				return
			}
		} else {
			msg := fmt.Sprintf("%s: %s", client.username, message)
			broadcastFrom(client, msg)
			client.messageCount++
		}
	}
}

func registerUser(client *Client, reader *bufio.Reader) error {
	msg := "Enter username: "
	sendToClient(client, msg)
	for {
		//fmt.Fprint(client.conn, "> ")
		username, err := receiveMessage(reader)
		if err != nil {
			return err
		}
		username = strings.ToUpper(username)
		if username == "" {
			msg := "Username cannot be empty! Enter a different username: "
			sendToClient(client, msg)
			continue
		}
		reply := make(chan bool)
		registerChan <- RegisterRequest{client: client, username: username, reply: reply}
		if <-reply {
			return nil
		}
		msg := "Username already in use! Enter a different username: "
		sendToClient(client, msg)
	}
}

func handleCommand(client *Client, command string) string {
	switch command {
	case "quit":
		msg := fmt.Sprintf("%s has left the chat.", client.username)
		broadcastFrom(client, msg)
		return "quit"
	case "users":
		sendToClient(client, formatConnectedUsers())
		return "users"
	case "stats":
		msg := fmt.Sprintf("You have sent %v messages so far in this room", client.messageCount)
		sendToClient(client, msg)
		return "stats"
	case "help":
		msg := "Available commands: \n- /quit \n- /users \n- /stats"
		sendToClient(client, msg)
		return "help"
	default:
		msg := "Command not found"
		sendToClient(client, msg)
		return ""
	}
}

func receiveMessage(reader *bufio.Reader) (string, error) {
	message, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Read error: %v", err)
		return "", err
	}

	return strings.TrimSpace(message), err
}

func sendToClient(client *Client, content string) {
	msgChan <- Message{
		sender:        client,
		content:       content,
		broadcastType: ToSender,
	}
}

func broadcastFrom(client *Client, content string) {
	msgChan <- Message{
		sender:        client,
		content:       content,
		broadcastType: ToAllButSender,
	}
}

func broadcastAll(content string) {
	msgChan <- Message{
		content:       content,
		broadcastType: ToAll,
	}
}

func sendToUser(sender, recipient *Client, content string) {
	msgChan <- Message{
		sender:        sender,
		recipient:     recipient,
		content:       content,
		broadcastType: ToUser,
	}
}
