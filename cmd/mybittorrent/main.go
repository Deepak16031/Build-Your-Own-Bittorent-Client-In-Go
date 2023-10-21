package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	bencode "github.com/jackpal/bencode-go"
	"log"
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
		url, length, sha1Hash := getTorrentInfo(fileContentString)
		fmt.Println("Tracker URL:", url)
		fmt.Println("Length:", length)
		fmt.Println("Info Hash:", sha1Hash)
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}

func getTorrentInfo(contentString string) (string, int, string) {
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
	return metadata.Announce, metadata.Info.Length, hashString
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
