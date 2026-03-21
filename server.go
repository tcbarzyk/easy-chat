package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

type BroadcastType int

const (
	ToAll BroadcastType = iota
	ToSender
	ToAllButSender
	ToUser
)

type Client struct {
	conn          net.Conn
	username      string
	numOfMessages int
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
	return &Client{
		conn:          conn,
		numOfMessages: 0,
	}
}

var clients = make(map[net.Conn]*Client)
var msgChan = make(chan Message)
var leaveChan = make(chan LeaveEvent)
var userListChan = make(chan UserListRequest)
var registerChan = make(chan RegisterRequest)

func main() {

	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatal("Error listening:", err)
	}

	defer listener.Close()

	log.Println("Server listening...")
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
		for conn := range clients {
			fmt.Fprintln(conn, msg.content)
			fmt.Fprint(conn, "> ")
		}
	case ToSender:
		fmt.Fprintln(msg.sender.conn, msg.content)
		fmt.Fprint(msg.sender.conn, "> ")
	case ToAllButSender:
		for conn := range clients {
			if conn != msg.sender.conn {
				fmt.Fprintln(conn, msg.content)
				fmt.Fprint(conn, "> ")
			}
		}
	case ToUser:
		if msg.recipient != nil {
			fmt.Fprintln(msg.recipient.conn, msg.content)
			fmt.Fprint(msg.recipient.conn, "> ")
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

func printConnectedUsers(client *Client) {
	reply := make(chan []string)
	userListChan <- UserListRequest{reply: reply}
	users := <-reply
	msg := fmt.Sprintf("There are currently %v connected users\n", len(users))
	fmt.Fprint(client.conn, msg)
	for _, username := range users {
		msg := fmt.Sprintf("- %s\n", username)
		fmt.Fprint(client.conn, msg)
	}
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

	fmt.Fprintln(conn, "Welcome to the chatroom.")
	printConnectedUsers(client)

	registerUser(client, reader)

	defer func() {
		leaveChan <- LeaveEvent{client: client}
		conn.Close()
	}()

	fmt.Fprintf(conn, "You have joined the chatroom as %s.\n", client.username)

	msg := fmt.Sprintf("%s has joined the chat.", client.username)
	msgChan <- Message{sender: client, content: msg, broadcastType: ToAllButSender}

	for {
		fmt.Fprint(conn, "> ")
		message, err := receiveMessage(reader)
		if err != nil {
			msg := fmt.Sprintf("%s has left the chat.", client.username)
			msgChan <- Message{sender: client, content: msg, broadcastType: ToAllButSender}
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
			msgChan <- Message{sender: client, content: msg, broadcastType: ToAllButSender}
			client.numOfMessages++
		}
	}
}

func registerUser(client *Client, reader *bufio.Reader) error {
	fmt.Fprintln(client.conn, "Enter username: ")
	foundUsername := false
	for !foundUsername {
		fmt.Fprint(client.conn, "> ")
		username, err := receiveMessage(reader)
		if err != nil {
			return err
		}
		username = strings.ToUpper(username)
		if username == "" {
			fmt.Fprintln(client.conn, "Username cannot be empty! Enter a different username: ")
			continue
		}
		reply := make(chan bool)
		registerChan <- RegisterRequest{client: client, username: username, reply: reply}
		success := <-reply
		if !success {
			fmt.Fprintln(client.conn, "Username already in use! Enter a different username: ")
		} else {
			foundUsername = true
		}
	}
	return nil
}

func handleCommand(client *Client, command string) string {
	switch command {
	case "quit":
		msg := fmt.Sprintf("%s has left the chat.", client.username)
		msgChan <- Message{sender: client, content: msg, broadcastType: ToAllButSender}
		return "quit"
	case "users":
		printConnectedUsers(client)
		return "users"
	case "stats":
		fmt.Fprintf(client.conn, "You have sent %v messages so far in this room\n", client.numOfMessages)
		return "stats"
	case "help":
		fmt.Fprintln(client.conn, "Available commands: \n- /quit \n- /users \n- /stats")
		return "help"
	default:
		fmt.Fprintln(client.conn, "Command not found")
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
