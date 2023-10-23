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
	"unicode"
)

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

		decoded, _, err := decodeBencode(bencodedValue)
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
		url, length, sha1Hash, pieceLength, pieces, _ := getTorrentInfo(fileContentString)
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
		_, peers, err := getTrackerResponse(filePath)
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
		_, _, _, _, _, infoHashRaw := getTorrentInfo(fileContentString)

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

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}

func getTorrentInfo(contentString string) (string, int, string, int, []string, []byte) {
	decodedData, _, err := decodeBencode(contentString)
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
	return metadata.Announce, metadata.Info.Length, hashString, metadata.Info.PieceLength, pieces, hashBytes
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

func getTrackerResponse(filePath string) (int, []string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	fileContentString := string(content)
	baseUrl, length, _, _, _, infoHashRaw := getTorrentInfo(fileContentString)
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
		return -1, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	respBenDict := string(body)
	decodedValue, _, err := decodeBencode(respBenDict)
	if err != nil {
		return -1, nil, err
	}
	dataMap := decodedValue.(map[string]interface{})

	peers := getPeersList(decodedValue)
	return dataMap["interval"].(int), peers, nil

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
	Internval int    `bencode:"interval"`
	Peers     string `bencode:"peers"`
}
