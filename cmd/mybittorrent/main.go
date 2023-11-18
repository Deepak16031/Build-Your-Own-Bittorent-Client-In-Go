package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	bencode "github.com/jackpal/bencode-go"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func decodeBencoded(bencodedString string) (interface{}, error) {
	decoded, err := bencode.Decode(strings.NewReader(bencodedString))
	if err != nil {
		return nil, fmt.Errorf("Only strings,ints, lists are supported at the moment, %q", bencodedString)
	}
	return decoded, nil
}

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decodeBencode(bencodedString string) (interface{}, int, error) {
	if len(bencodedString) == 0 {
		return nil, 1, nil
	}
	if unicode.IsDigit(rune(bencodedString[0])) {
		var firstColonIndex int

		for i := 0; i < len(bencodedString); i++ {
			if bencodedString[i] == ':' {
				firstColonIndex = i
				break
			}
		}

		lengthStr := bencodedString[:firstColonIndex]

		length, err := strconv.Atoi(lengthStr)
		if err != nil {
			return "", 1, err
		}

		return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], firstColonIndex + length, nil
	} else if bencodedString[0] == 'i' {
		var firstEIndex int
		for i := 0; i < len(bencodedString); i++ {
			if bencodedString[i] == 'e' {
				firstEIndex = i
				break
			}
		}
		intStr := bencodedString[1:firstEIndex]
		intValue, err := strconv.Atoi(intStr)
		if err != nil {
			return "", 1, err
		}

		return intValue, firstEIndex, nil
	} else if bencodedString[0] == 'l' {
		list, lastIndexCovered, err := decodeBencodedList(bencodedString[1:])
		return list, lastIndexCovered - 1, err
	} else if bencodedString[0] == 'd' {
		dict := make(map[string]interface{}, 0)
		indx := 1
		for bencodedString[indx] != 'e' && len(bencodedString[indx:]) != 1 {
			k, KeyLastIndexCovered, err := decodeBencode(bencodedString[indx:])
			if err != nil {
				return invalidDecodeType(bencodedString[indx:])
			}
			var key string
			if k, ok := k.(string); !ok {
				return "", KeyLastIndexCovered, fmt.Errorf("expected string key but got %q", k)
			} else {
				key = k
			}
			indx += KeyLastIndexCovered + 1
			v, valueLastIndexCovered, err := decodeBencode(bencodedString[indx:])
			if err != nil {
				return invalidDecodeType(bencodedString[indx:])
			}
			indx += valueLastIndexCovered + 1
			dict[key] = v
		}
		return dict, indx, nil
	} else if bencodedString[0] == 'e' {
		return decodeBencode(bencodedString[1:])
	} else {
		return invalidDecodeType(bencodedString)
	}
}

func invalidDecodeType(bencodedString string) (interface{}, int, error) {
	return "", 1, fmt.Errorf("Only strings,ints, lists are supported at the moment, %q", bencodedString)
}

func decodeBencodedList(bencodedString string) (interface{}, int, error) {
	list := []interface{}{}
	i := 0
	for i < len(bencodedString) {
		decodedValue, lastIndexCovered, err := decodeBencode(bencodedString[i:])
		if err != nil {
			return "", lastIndexCovered, err
		}
		if decodedValue != nil {
			list = append(list, decodedValue)
		}
		i += lastIndexCovered + 1
		for i < len(bencodedString) && bencodedString[i] == 'e' {
			i++
		}
	}
	return list, i - 1, nil
}

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	//fmt.Println("Logs from your program will appear here!")

	command := os.Args[1]

	if command == "decode" {
		// Uncomment this block to pass the first stage

		bencodedValue := os.Args[2]

		decoded, err := decodeBencoded(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else if command == "info" {
		// Get the path to the file from the command-line argument.
		filePath := os.Args[2]

		// Read the entire contents of the file into a string.
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatal(err)
		}

		// Convert the byte slice to a string.
		fileContentString := string(content)

		// Print the file contents as a string.
		//fmt.Println("File contents as a string:")
		//fmt.Println(fileContentString)
		torrentInfo := getTorrentInfo(fileContentString)
		url, length, sha1Hash, pieceLength, pieces :=
			torrentInfo.Announce, torrentInfo.TotalLength,
			torrentInfo.InfoHash, torrentInfo.PieceLength,
			torrentInfo.Pieces
		fmt.Println("Tracker URL:", url)
		fmt.Println("Length:", length)
		fmt.Println("Info Hash:", sha1Hash)
		fmt.Println("Piece Length:", pieceLength)
		fmt.Println("Piece Hashes:")
		for _, value := range pieces {
			fmt.Println(value)
		}
	} else if command == "peers" {
		filePath := os.Args[2]
		peers, err := getTrackerResponse(filePath)
		if err != nil {
			fmt.Println("Unable to fetch tracker data :", err)
		}
		fmt.Println()
		for _, value := range peers {
			fmt.Println(value)
		}
	} else if command == "handshake" {
		filePath := os.Args[2]
		address := os.Args[3]

		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatal(err)
		}

		// Convert the byte slice to a string.
		fileContentString := string(content)
		infoHashRaw := getTorrentInfo(fileContentString).InfoHash

		var length uint8 = 19
		var protocol []byte = []byte("BitTorrent protocol")
		reservedBytes := make([]byte, 8)
		shaInfoHash := []byte(infoHashRaw)
		peerId := []byte("00112233445566778899")

		var request []byte
		request = append(request, length)
		request = append(request, protocol...)
		request = append(request, reservedBytes...)
		request = append(request, shaInfoHash...)
		request = append(request, peerId...)

		conn, err := net.Dial("tcp", address)
		if err != nil {
			fmt.Println("Failed to establish tcp connection with - ", address)
		}
		conn.Write(request)

		// Receive a response from the server
		buffer := make([]byte, 128)
		_, err = conn.Read(buffer)

		if err != nil {
			fmt.Println("Error receiving buffer", err)
		}
		// Print the ACK.
		ackPeerIdBuffer := buffer[48:68]
		peerIdHex := fmt.Sprintf("%x", ackPeerIdBuffer)
		fmt.Println("Peer ID:", peerIdHex)
		conn.Close()
	} else if command == "download_piece" {
		filePath := os.Args[2]
		address := os.Args[3]

		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatal(err)
		}

		// Convert the byte slice to a string.
		fileContentString := string(content)
		torrentInfo := getTorrentInfo(fileContentString)

		var length uint8 = 19
		var protocol []byte = []byte("BitTorrent protocol")
		reservedBytes := make([]byte, 8)
		shaInfoHash := []byte(torrentInfo.RawInfoHash)
		peerId := []byte("00112233445566778899")

		var request []byte
		request = append(request, length)
		request = append(request, protocol...)
		request = append(request, reservedBytes...)
		request = append(request, shaInfoHash...)
		request = append(request, peerId...)

		conn, err := net.Dial("tcp", address)
		defer conn.Close()
		if err != nil {
			log.Fatal("Failed to establish tcp connection with - ", address)
		}

		conn.Write(request)

		// Receive a response from the server
		buffer := make([]byte, 68)
		_, err = conn.Read(buffer)

		if err != nil {
			fmt.Println("Error receiving buffer", err)
		}
		// Print the ACK.
		ackPeerIdBuffer := buffer[48:68]
		peerIdHex := fmt.Sprintf("%x", ackPeerIdBuffer)
		fmt.Println("Handshake established with - ", peerIdHex)

		if err != nil {
			log.Fatalf("%v", err)
		}
		buffer = make([]byte, 6)
		conn.Read(buffer)
		//msg, _ := waitForMessage(conn)
		//if msg.Id != bitfield {
		//	log.Fatalf("Expected bitfield message as first message")
		//}

		// send interested msg
		msgToSent := PeerMessage{
			PayloadLength: 1,
			Id:            interested,
			Payload:       nil,
		}
		sendMessage(conn, msgToSent)

		//wait for unchoke msg
		//msg, err = waitForMessage(conn)
		buffer = make([]byte, 5)
		conn.Read(buffer)
		if err != nil {
			log.Fatalf("Error occured for waiting message for unchoke %v", err)
		}
		//if msg.Id != unchoke {
		//	log.Fatalf("Expected unchoke message as second message")
		//}

		numberOfPieces := len(torrentInfo.Pieces)
		// all the piece except last one is straight forward
		// send request for each piece block wise
		// store in an byte slice, append each
		// save it
		blockSize := 16384
		for i := 0; i < numberOfPieces-1; i++ {
			block := []byte{}
			for j := 0; j < torrentInfo.PieceLength/blockSize; j++ {
				msgToSent = PeerMessage{
					PayloadLength: 0,
					Id:            MessageId(6),
					Payload:       nil,
				}
				payload := intToBytes(i)
				payload = append(payload, intToBytes(blockSize*j)...)
				payload = append(payload, intToBytes(blockSize)...)
				msgToSent.PayloadLength = 13
				msgToSent.Payload = payload
				err := sendMessage(conn, msgToSent)

				if err != nil {
					log.Fatalf("Error sending block request : %v", msgToSent)
				}
				buffer = make([]byte, 16397)

				conn.Read(buffer)
				//msg, err := waitForMessage(conn)
				//if msg.Id != piece {
				//	log.Fatalf("Expected PIECE msg for the msgSent - %+v, received msg %+v", msgToSent, msg)
				//}
				block = append(block, buffer[13:]...)
			}
			//buffer = make([]byte, 1000000)
			//conn.Read(buffer)
			//check hash
			//
			writer := bytes.NewBuffer([]byte{})
			err = bencode.Marshal(writer, block)
			if err != nil {
				fmt.Println(err)
			}
			sha1Hash := sha1.New()
			sha1Hash.Write(writer.Bytes())
			blockHash := sha1Hash.Sum(nil)
			if fmt.Sprintf("%x", blockHash) != torrentInfo.Pieces[i] {
				log.Fatalf("Hashes are not equal \n blockhash-%x \n pieceHash - %v", blockHash, torrentInfo.Pieces[i])
			}
		}
		buffer = make([]byte, 100000)

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}

}

func waitForMessage(conn net.Conn) (*PeerMessage, error) {

	// TODO make 30 a constant
	timer := time.NewTimer(30 * time.Second)
	msgChan := make(chan *PeerMessage)

	go func() {
		peerMessage := PeerMessage{
			PayloadLength: -1,
			Id:            100,
			Payload:       make([]uint8, 0),
		}

		//message length
		//var messageLength int32
		//TODO put error handling
		buffer := make([]byte, 4)
		n, err := conn.Read(buffer)
		if err != nil {
			log.Fatalf("not enough data to read? number of bytes:%v, err- %v", n, err)
		}
		err = binary.Read(bytes.NewReader(buffer), binary.BigEndian, &peerMessage.PayloadLength)
		if err != nil {
			fmt.Println("cant read length: ", err)
		}
		buffer = make([]byte, 1)
		conn.Read(buffer)
		binary.Read(bytes.NewReader(buffer), binary.BigEndian, &peerMessage.Id)

		if peerMessage.PayloadLength > 1 {
			buffer = make([]byte, peerMessage.PayloadLength-1)
			conn.Read(buffer)
			peerMessage.Payload = make([]uint8, peerMessage.PayloadLength-1)
			binary.Read(bytes.NewReader(buffer), binary.BigEndian, &peerMessage.Payload)
		}

		msgChan <- &peerMessage
	}()

	select {
	case <-timer.C:
		return nil, fmt.Errorf("timed Out, no msg received")
	case msg := <-msgChan:
		return msg, nil
	}

	// now return the message

	return nil, fmt.Errorf("Something went horrible")
}

func sendMessage(conn net.Conn, message PeerMessage) error {
	timer := time.NewTimer(30 * time.Second)
	doneChan := make(chan bool)
	errorChan := make(chan error)
	go func() {
		var buffer bytes.Buffer
		if err := binary.Write(&buffer, binary.BigEndian, message.PayloadLength); err != nil {
			errorChan <- fmt.Errorf("Unable to write message length to buffer: %w", err)
			return
		}
		if err := binary.Write(&buffer, binary.BigEndian, message.Id); err != nil {
			errorChan <- fmt.Errorf("Unable to write message id to buffer: %w", err)
			return
		}
		if err := binary.Write(&buffer, binary.BigEndian, message.Payload); err != nil {
			errorChan <- fmt.Errorf("Unable to write message payload to buffer: %w", err)
			return
		}
		_, err := conn.Write(buffer.Bytes())
		if err != nil {
			errorChan <- fmt.Errorf("Unable to write message to peer: %w", err)
			return
		}
		doneChan <- true
	}()
	select {
	case <-timer.C:
		return fmt.Errorf("Timeout while sending message: %v", message)
	case err := <-errorChan:
		return err
	case <-doneChan:
		return nil
	}
}

// getTorrentInfo extracts relevant information from a Bencode-encoded torrent metadata string.
// It returns the following information:
//   - Announce URL: the URL where the torrent tracker is located.
//   - Total Length: the total size of the torrent content in bytes.
//   - Info Hash: the SHA-1 hash of the torrent info.
//   - Piece Length: the size of each piece in bytes.
//   - Pieces: a slice of piece hashes.
//   - Hash Bytes: the raw hash bytes of the torrent info.
func getTorrentInfo(contentString string) TorrentInfo {
	decodedData, err := decodeBencoded(contentString)
	if err != nil {
		fmt.Printf("Invalid decoding string to fetch info %v", err)
	}

	// process of marshalling a bencoding to a struct
	marshalledBytes := bytes.NewBuffer([]byte{})
	err = bencode.Marshal(marshalledBytes, decodedData)
	if err != nil {
		fmt.Println(err)
	}
	metadata := Metadata{}
	bencode.Unmarshal(marshalledBytes, &metadata)
	//fmt.Println("metadata info: ", metadata.Info)
	if err != nil {
		fmt.Println(err)
	}

	writer := bytes.NewBuffer([]byte{})
	err = bencode.Marshal(writer, metadata.Info)
	if err != nil {
		fmt.Println(err)
	}
	sha1Hash := sha1.New()
	sha1Hash.Write(writer.Bytes())
	hashBytes := sha1Hash.Sum(nil)
	hashString := fmt.Sprintf("%x", hashBytes)

	pieces, err := getPieces(metadata.Info.Pieces)
	torrentInfo := TorrentInfo{
		Announce:    metadata.Announce,
		TotalLength: metadata.Info.Length,
		InfoHash:    hashString,
		PieceLength: metadata.Info.PieceLength,
		Pieces:      pieces,
		RawInfoHash: hashBytes,
	}
	//return metadata.Announce, metadata.Info.Length, hashString, metadata.Info.PieceLength, pieces, hashBytes
	return torrentInfo
}

func getPieces(pieces string) ([]string, error) {
	piecesList := make([]string, 0)
	if len(pieces)%20 != 0 {
		return nil, fmt.Errorf("Not a multiple of 20")
	}
	i := 0
	for i+20 <= len(pieces) {
		sha1Hash := pieces[i : i+20]
		hexadecimal_string := hex.EncodeToString([]byte(sha1Hash))
		piecesList = append(piecesList, hexadecimal_string)
		i += 20
	}
	return piecesList, nil
}

func getTrackerResponse(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	fileContentString := string(content)
	torrentInfo := getTorrentInfo(fileContentString)

	baseUrl, length, infoHashRaw := torrentInfo.Announce, torrentInfo.TotalLength, torrentInfo.RawInfoHash
	params := url.Values{}
	params.Add("info_hash", string(infoHashRaw))
	params.Add("peer_id", "00112233445566778899")
	params.Add("port", "6881")
	params.Add("uploaded", strconv.Itoa(0))
	params.Add("downloaded", strconv.Itoa(0))
	params.Add("left", strconv.Itoa(length))
	params.Add("compact", strconv.Itoa(1))

	requestUrl := baseUrl + "?" + params.Encode()
	resp, err := http.Get(requestUrl)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	respBenDict := string(body)
	decodedValue, err := decodeBencoded(respBenDict)
	if err != nil {
		return nil, err
	}

	peers := getPeersList(decodedValue)
	return peers, nil

}

func getPeersList(decodedValue interface{}) []string {
	marshalledBytes := bytes.NewBuffer([]byte{})
	err := bencode.Marshal(marshalledBytes, decodedValue)
	if err != nil {
		fmt.Println(err)
	}
	trackerResponse := TrackerResponse{}
	bencode.Unmarshal(marshalledBytes, &trackerResponse)

	var peersList []string
	// convert to byte array

	byteArr := []byte(trackerResponse.Peers)

	i := 0
	for i+6 <= len(byteArr) {
		address := fmt.Sprintf("%v.%v.%v.%v", byteArr[i], byteArr[i+1], byteArr[i+2], byteArr[i+3])
		port := binary.BigEndian.Uint16(byteArr[i+4 : i+6])
		peersList = append(peersList, fmt.Sprintf("%s:%d", address, port))
		i += 6
	}
	return peersList
}

type Metadata struct {
	Announce string       `bencode:"announce"`
	Info     MetadataInfo `bencode:"info"`
}
type MetadataInfo struct {
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
	PieceLength int    `bencode:"piece length"`
	Pieces      string `bencode:"pieces"`
}

type TrackerResponse struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

type PeerMessage struct {
	PayloadLength int32
	Id            MessageId
	Payload       []uint8
}

type MessageId uint8

const (
	choke      MessageId = 0
	unchoke              = 1
	interested           = 2
	have                 = 4
	bitfield             = 5
	request              = 6
	piece                = 7
	cancel               = 8
)

//func encodePeerMessage(message PeerMessage) ([]byte, error) {
//	// Encode PayloadLength (4 bytes, big-endian)
//	var payloadLengthBytes [4]byte
//	binary.BigEndian.PutUint32(payloadLengthBytes[:], binary.BigEndian.Uint32(message.PayloadLength))
//
//	// Encode the MessageId (1 byte)
//	messageIdByte := byte(message.Id)
//
//	// Concatenate the encoded fields
//	encodedMessage := append(payloadLengthBytes[:], messageIdByte)
//	encodedMessage = append(encodedMessage, message.Payload...)
//
//	return encodedMessage, nil
//}

type TorrentInfo struct {
	Announce    string
	TotalLength int
	InfoHash    string
	PieceLength int
	Pieces      []string
	RawInfoHash []byte
}

func intToBytes(num int) []uint8 {
	bytes := make([]uint8, 4)
	binary.BigEndian.PutUint32(bytes, uint32(num))
	return bytes
}
