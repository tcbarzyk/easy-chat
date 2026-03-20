package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

//issue: currently, a user will still be part of the clients map if they have connected but not selected a username. this may lead to behavior such as, when a user types /users and there is another user online who has not selected a username, it will display (for example) "2 users online" but only print 1 username

type Client struct {
	Conn          net.Conn
	Username      string
	NumOfMessages int
}

func NewClient(conn net.Conn) *Client {
	return &Client{
		Conn:          conn,
		NumOfMessages: 0,
	}
}

var mu sync.Mutex
var clients = make(map[net.Conn]*Client)

func main() {

	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatal("Error listening:", err)
	}

	defer listener.Close()

	log.Println("Server listening...")

	for {

		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accepting conn:", err)
			continue
		}

		go handleConnection(conn)
	}
}

func broadcast(msg string, sender net.Conn) {
	mu.Lock()
	defer mu.Unlock()
	for client := range clients {
		if client != sender {
			fmt.Fprintln(client, msg)
			fmt.Fprint(client, "> ")
		}
	}
}

func printConnectedUsers(conn net.Conn) {
	mu.Lock()
	printedAUser := false
	for _, c := range clients {
		if c.Username != "" {
			fmt.Fprintf(conn, "- %s \n", c.Username)
			printedAUser = true
		}
	}
	if !printedAUser {
		fmt.Fprintln(conn, "(no connected users)")
	}
	mu.Unlock()
}

func usernameExists(username string) bool {
	mu.Lock()
	defer mu.Unlock()
	for _, c := range clients {
		if username == c.Username {
			return true
		}
	}
	return false
}

func handleConnection(conn net.Conn) {

	defer func() {
		conn.Close()
		mu.Lock()
		delete(clients, conn)
		mu.Unlock()
	}()

	reader := bufio.NewReader(conn)

	client := NewClient(conn)

	mu.Lock()
	clients[conn] = client
	mu.Unlock()

	fmt.Fprintln(conn, "Welcome to the chatroom. Connected users: ")
	printConnectedUsers(conn)
	fmt.Fprintln(conn, "Enter username: ")
	foundUsername := false
	for !foundUsername {
		fmt.Fprint(conn, "> ")
		username, err := receiveMessage(reader)
		if err != nil {
			return
		}
		username = strings.ToUpper(username)
		if username == "" {
			fmt.Fprintln(conn, "Username cannot be empty! Enter a different username: ")
		} else if usernameExists(username) {
			fmt.Fprintln(conn, "Username already in use! Enter a different username: ")
		} else {
			foundUsername = true
			mu.Lock()
			client.Username = username
			mu.Unlock()
		}
	}

	fmt.Fprintf(conn, "You have joined the chatroom as %s.\n", client.Username)

	msg := fmt.Sprintf("%s has joined the chat.", client.Username)
	broadcast(msg, conn)

	for {
		fmt.Fprint(conn, "> ")
		message, err := receiveMessage(reader)
		if err != nil {
			msg := fmt.Sprintf("%s has left the chat.", client.Username)
			broadcast(msg, conn)
			return
		} else if message == "" {
			continue
		} else if strings.ToLower(message) == "/quit" {
			msg := fmt.Sprintf("%s has left the chat.", client.Username)
			broadcast(msg, conn)
			return
		} else if strings.ToLower(message) == "/users" {
			mu.Lock()
			fmt.Fprintf(conn, "There are currently %v connected users\n", len(clients))
			mu.Unlock()
			printConnectedUsers(conn)
		} else if strings.ToLower(message) == "/stats" {
			fmt.Fprintf(conn, "You have sent %v messages so far in this room\n", client.NumOfMessages)
		} else {
			msg := fmt.Sprintf("%s: %s", client.Username, message)
			client.NumOfMessages++
			broadcast(msg, conn)
		}
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
