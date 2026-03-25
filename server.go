package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"tcbarzyk.dev/chat-server/pkg/buffer"
)

type BroadcastType int

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
	sender            *Client
	recipientUsername string
	content           string
	broadcastType     BroadcastType
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
		writeChan:    make(chan string, 100),
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
var msgChan = make(chan Message, 100)
var leaveChan = make(chan LeaveEvent)
var userListChan = make(chan UserListRequest)
var registerChan = make(chan RegisterRequest)

var history = buffer.NewRingBuffer[*Message](50)

func main() {
	debugMode := flag.Bool("debug", false, "enable pprof server on localhost:6060")
	port := flag.Int("port", 9000, "The port for the chat server to listen on")
	flag.Parse()
	if *debugMode {
		go func() {
			log.Println("Debug mode enabled. pprof listening on localhost:6060")
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				log.Printf("Debug server error: %v", err)
			}
		}()
	}
	address := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal("Error listening:", err)
	}

	defer listener.Close()

	log.Printf("Server listening on %s...\n", address)
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
		msgCopy := msg
		history.Write(&msgCopy)
		for _, client := range clients {
			client.writeChan <- msg.content
		}
	case ToSender:
		msg.sender.writeChan <- msg.content
	case ToAllButSender:
		msgCopy := msg
		history.Write(&msgCopy)
		for _, client := range clients {
			if client.conn != msg.sender.conn {
				client.writeChan <- msg.content
			}
		}
	case ToUser:
		recipient := findClientByUsername(msg.recipientUsername)
		if recipient == nil {
			msg.sender.writeChan <- "Error: recipient not found"
			return
		}
		recipient.writeChan <- msg.content
	}
}

func findClientByUsername(username string) *Client {
	for _, c := range clients {
		if c.username == username {
			return c
		}
	}
	return nil
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

func displayMessageHistory(c *Client) {
	for _, msg := range history.GetAll() {
		c.Send(msg.content)
	}
}

func handleConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)
	client := NewClient(conn)

	registered := false

	defer func() {
		if registered {
			broadcastFrom(client, fmt.Sprintf("%s has left the chat.", client.username))
			leaveChan <- LeaveEvent{client: client}
		} else {
			close(client.writeChan)
		}
	}()

	client.Send("Welcome to the chatroom.")

	client.Send(formatConnectedUsers())

	err := registerUser(client, reader)
	if err != nil {
		return
	}

	registered = true

	client.Send(fmt.Sprintf("You have joined the chatroom as %s.", client.username))

	client.Send("--------BEGIN CHAT--------")

	displayMessageHistory(client)

	broadcastFrom(client, fmt.Sprintf("%s has joined the chat.", client.username))

	for {
		//fmt.Fprint(conn, "> ")
		message, err := receiveMessage(reader)
		if err != nil {
			//err is handled by recieveMessage
			return
		} else if message == "" {
			continue
		} else if strings.HasPrefix(message, "/") {
			shouldQuit, err := handleCommand(client, message[1:])
			if err != nil {
				broadcastToSender(client, fmt.Sprintf("Error: %v", err))
			}
			if shouldQuit {
				return
			}
		} else {
			broadcastFrom(client, fmt.Sprintf("%s: %s", client.username, message))
			client.messageCount++
		}
	}
}

func registerUser(client *Client, reader *bufio.Reader) error {
	client.Send("Enter username: ")
	for {
		username, err := receiveMessage(reader)
		if err != nil {
			return err
		}
		username = strings.ToUpper(username)
		if username == "" {
			client.Send("Username cannot be empty! Enter a different username: ")
			continue
		}
		reply := make(chan bool)
		registerChan <- RegisterRequest{client: client, username: username, reply: reply}
		if <-reply {
			return nil
		}
		client.Send("Username already in use! Enter a different username: ")
	}
}

var (
	ErrEmptyCommand   = errors.New("command cannot be empty")
	ErrUnknownCommand = errors.New("command not found")
	ErrInvalidSyntax  = errors.New("invalid syntax")
	ErrSelfMessage    = errors.New("you cannot send a private message to yourself")
)

func handleCommand(client *Client, command string) (bool, error) {
	command = strings.TrimSpace(command)
	if len(command) == 0 {
		return false, ErrEmptyCommand
	}
	args := strings.Fields(command)
	cmd := strings.ToLower(args[0])
	switch cmd {
	case "quit":
		return true, nil
	case "users":
		broadcastToSender(client, formatConnectedUsers())
		return false, nil
	case "stats":
		broadcastToSender(client, fmt.Sprintf("You have sent %v messages so far in this room", client.messageCount))
		return false, nil
	case "help":
		broadcastToSender(client, "Available commands: \n- /quit \n- /users \n- /stats\n- /msg <user> <body>")
		return false, nil
	case "msg":
		if len(args) < 3 {
			return false, fmt.Errorf("%w: usage is /msg <user> <body>", ErrInvalidSyntax)
		}
		recipientUsername := strings.ToUpper(args[1])
		if recipientUsername == client.username {
			return false, ErrSelfMessage
		}
		msg := fmt.Sprintf("Message from %s: %s", client.username, strings.Join(args[2:], " "))
		sendToUser(client, recipientUsername, msg)
		return false, nil
	default:
		return false, ErrUnknownCommand
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

func broadcastToSender(client *Client, content string) {
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

func sendToUser(sender *Client, recipientUsername string, content string) {
	msgChan <- Message{
		sender:            sender,
		recipientUsername: recipientUsername,
		content:           content,
		broadcastType:     ToUser,
	}
}

func (c *Client) Send(content string) bool {
	select {
	case c.writeChan <- content:
		return true
	default:
		log.Printf("Dropped message for %s (buffer full)", c.username)
		return false
	}
}
