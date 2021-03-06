package main

import (
	"net"
	log "github.com/Sirupsen/logrus"
	"github.com/vmihailenco/msgpack"
	"os"
	"io"
	"path/filepath"
)

var done chan bool

func (s *Server) setupServerConnection(address string) {

	//also listen from requests from the server on a random port
	listeningAddress, err := net.ResolveUDPAddr("udp4", ":0")
	errorCheck(err, "setupConnection", true)
	log.Info("...CONNECTED! ")

	s.connection, err = net.ListenUDP("udp4", listeningAddress)
	errorCheck(err, "setupConnection", true)

	log.Printf("listening on: local:%s\n", s.connection.LocalAddr())

}

func (s *Server) readFromSocket(buffersize int) {
	for {
		var b = make([]byte, buffersize)
		n, addr, err := s.connection.ReadFromUDP(b[0:])
		errorCheck(err, "readFromSocket", false)

		s.client = addr

		b = b[0:n]
		if n > 0 {
			pack := packet{b, addr}
			select {
			case s.packets <- pack:
				continue
			case <-s.kill:
				break
			}
		}

		//TODO: is this needed ?
		select {
		case <-s.kill:
			break
		default:
			continue
		}
	}
}

func (s *Server) processPackets() {
	for pack := range s.packets {
		var msg Message
		err := msgpack.Unmarshal(pack.bytes, &msg)
		errorCheck(err, "processPackets", false)
		s.messages <- msg

		//log.Println("<<<  SERVER GOT")
		//spew.Dump(msg)
	}
}

func (s *Server) processMessages() {

	done = make(chan bool, 1)

	for msg := range s.messages {
		switch msg.Opcode {
		case RRQ:
			log.Printf("RRQ for file %s with payload %s", msg.Filename,
				string(msg.Message))

			dir, _ := os.Getwd()
			fullFilePath := dir + "/myfiles/" + msg.Filename

			if _, err := os.Stat(fullFilePath); err == nil {
				log.Info("sending file " + msg.Filename + " to the client. Hash: " + Sha256Sum(fullFilePath))
				go s.sendFileToClient(fullFilePath)
			} else {
				log.Info("cannot find "+msg.Filename+" on the server.")
				go s.Send(ERROR, "", []byte("cannot find "+msg.Filename+" on the server."))
			}

		case WRQ:
			log.Printf("WRQ for file %s with payload %s", msg.Filename, string(msg.Message))
			CreateDirIfNotExist("myfiles")

			//Will replace it if already exists
			var file, err = os.Create("myfiles" + string(os.PathSeparator) + msg.Filename)
			errorCheck(err, "creating a new file", false)
			defer file.Close()

			s.Send(ACK, msg.Filename, []byte("WRQ"))

		case DATA:
			s.WriteBytesToFile(msg.Filename, msg.Message)
		case ACK:
			if string(msg.Message) == "WRQ" {
				//sending file to the client

				dir, _ := os.Getwd()
				fullFilePath := dir + "/aftp/server/myfiles/" + msg.Filename
				log.Info("sending file " + msg.Filename + " to the client. Hash: " + Sha256Sum(fullFilePath))

				if _, err := os.Stat(fullFilePath); err == nil {
					go s.sendFileToClient(fullFilePath)
				}
			} else {
				done <- true
			}

		case ERROR:
			log.Printf("Error for file %s [%s]", msg.Filename, string(msg.Message))
		case SEND_COMPLETED:
			log.Printf("SEND_COMPLETED for file %s with hash: %s", msg.Filename, string(msg.Message))
			//got a send completed from the client. Issue a received ok
			s.Send(RECEIVED_OK, msg.Filename, []byte(Sha256Sum("myfiles/"+msg.Filename)))
		case RECEIVED_OK:
			log.Printf("RECEIVED_OK for file %s with hash: %s", msg.Filename, string(msg.Message))
		case LIST_ALL:
			log.Printf("Got a list all request from the client. Listing....")
			s.Send(LIST_ALL, "", ListAllFiles("myfiles"))
		default:
			log.Warnln("incorrect or not implemented opcode")
		}
	}
}

func (s *Server) WriteBytesToFile(filename string, payload []byte) {
	f, err := os.OpenFile("myfiles/"+filename, os.O_APPEND|os.O_WRONLY, 0644)
	errorCheck(err, "WriteBytesToFile", false)
	_, err = f.Write(payload)
	errorCheck(err, "WriteBytesToFile", false)

	defer s.Send(ACK, filename, nil)
	defer f.Close()
}

func (s *Server) sendFileToClient(fullPathFile string) {

	file, err := os.Open(fullPathFile)
	if err != nil {
		log.Warn(err)
		return
	}
	defer file.Close()
	buffer := make([]byte, opts.Buffer)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Warn(err)
			}
			break
		}
		s.Send(DATA, filepath.Base(fullPathFile), buffer[:n])

		// wait for ACK to write on channel done
		<-done
	}

	s.Send(SEND_COMPLETED, filepath.Base(fullPathFile), []byte(Sha256Sum(fullPathFile)))

}

func (s *Server) Send(opcode MessageType, filename string, payload []byte) {

	msg := Message{
		opcode, filename, payload,
	}

	//log.Println(">>> SERVER SENDING >>> ")
	//spew.Dump(msg)

	b, err := msgpack.Marshal(msg)
	errorCheck(err, "Send", false)

	_, err = s.connection.WriteToUDP(b, s.client)
	errorCheck(err, "Send", false)
}
