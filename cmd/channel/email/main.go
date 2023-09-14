package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/channel"
	"log"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
)

type config struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
	From string `json:"from"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	line, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal("Email: Failed to read config:", err)
	}

	c := config{}
	err = json.Unmarshal([]byte(line), &c)
	if err != nil {
		log.Fatal("Email: Failed to parse json-encoded config", err)
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Email: Failed to read request", err)
		}

		req, err := prepareReq(line)
		if err != nil {
			log.Fatal("Email: Failed to parse json-encoded request", err)
		}

		err = c.Send(req)
		if err != nil {
			marshal, err := json.Marshal(map[string]any{"Success": false, "Error": err})
			if err != nil {
				log.Fatal("Email failed: Cant prepare json response")
			}
			_, _ = fmt.Fprintln(os.Stdout, marshal)
		} else {
			marshal, err := json.Marshal(map[string]any{"Success": true, "Error": nil})
			if err != nil {
				log.Fatal("Email failed: Cant prepare json response")
			}
			_, err = fmt.Fprintln(os.Stdout, marshal)
			if err != nil {
				log.Fatal("Email sent but response failed")
			}
		}
	}
}

func prepareReq(jsonStr string) (channel.NotificationRequest, error) {
	var req channel.NotificationRequest
	err := json.Unmarshal([]byte(jsonStr), &req)
	if err != nil {
		return channel.NotificationRequest{}, err
	}

	return req, nil
}

func (c *config) Send(req channel.NotificationRequest) error {
	var to []string
	for _, address := range req.Contact.Addresses {
		if address.Type == "email" {
			to = append(to, address.Address)
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("contact user %s doesn't have an e-mail address", req.Contact.FullName)
	}

	var msg bytes.Buffer
	_, _ = fmt.Fprintf(&msg, "To: %s\n", strings.Join(to, ","))
	_, _ = fmt.Fprintf(&msg, "Subject: [#%d] %s %s is %s\n\n", req.Incident.Id, req.Event.Type, req.Incident.ObjectDisplayName, req.Event.Severity.String())

	channel.FormatMessage(&msg, req.Incident, req.Event, req.IcingaWeb2Url)

	err := smtp.SendMail(c.GetServer(), nil, c.From, to, bytes.ReplaceAll(msg.Bytes(), []byte("\n"), []byte("\r\n")))
	if err != nil {
		return err
	}

	return nil
}

func (c *config) GetServer() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port)))
}
