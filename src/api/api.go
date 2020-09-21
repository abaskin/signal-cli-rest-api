package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"strings"

	"github.com/abaskin/signald-go/signald"
	"github.com/gin-gonic/gin"
	"github.com/h2non/filetype"
	log "github.com/sirupsen/logrus"
	qrcode "github.com/skip2/go-qrcode"
)

const groupPrefix = "group."

type GroupEntry struct {
	Name       string   `json:"name"`
	ID         string   `json:"id"`
	InternalID string   `json:"internal_id"`
	Members    []string `json:"members"`
	Active     bool     `json:"active"`
	Blocked    bool     `json:"blocked"`
}

type RegisterNumberRequest struct {
	UseVoice bool `json:"use_voice"`
}

type VerifyNumberSettings struct {
	Pin string `json:"pin"`
}

type SendMessageV1 struct {
	Number           string   `json:"number"`
	Recipients       []string `json:"recipients"`
	Message          string   `json:"message"`
	Base64Attachment string   `json:"base64_attachment"`
	IsGroup          bool     `json:"is_group"`
}

type SendMessageV2 struct {
	Number            string   `json:"number"`
	Recipients        []string `json:"recipients"`
	Message           string   `json:"message"`
	Base64Attachments []string `json:"base64_attachments"`
}

type Error struct {
	Msg string `json:"error"`
}

type About struct {
	SupportedAPIVersions []string `json:"versions"`
	BuildNr              int      `json:"build"`
}

type CreateGroup struct {
	ID string `json:"id"`
}

func convertInternalGroupIDToGroupID(internalID string) string {
	return groupPrefix + base64.StdEncoding.EncodeToString([]byte(internalID))
}

func (a *Api) send(c *gin.Context, number string, message string, recipients []string,
	base64Attachments []string, isGroup bool) {

	if len(recipients) == 0 {
		c.JSON(400, gin.H{"error": "Please specify at least one recipient"})
		return
	}

	groupID := ""
	if isGroup {
		if len(recipients) > 1 {
			c.JSON(400, gin.H{"error": "More than one group is currently not allowed"})
			return
		}

		if _, err := base64.StdEncoding.DecodeString(recipients[0]); err != nil {
			c.JSON(400, gin.H{"error": "Invalid group id"})
			return
		}

		groupID = recipients[0]
		recipients[0] = ""
	}

	attachments := []signald.RequestAttachment{}
	for _, base64Attachment := range base64Attachments {
		dec, err := base64.StdEncoding.DecodeString(base64Attachment)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		fType, err := filetype.Get(dec)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		f, err := ioutil.TempFile(a.attachmentTmpDir, "signald-rest-api-*."+fType.Extension)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		defer os.Remove(f.Name())
		defer f.Close()
		attachments = append(attachments, signald.RequestAttachment{
			Filename: f.Name(),
		})

		if _, err := f.Write(dec); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if err := f.Sync(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		f.Close()
	}

	for _, to := range recipients {
		_, err := a.s.Send(number, signald.RequestAddress{Number: to},
			groupID, message, attachments, signald.RequestQuote{})

		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(201, nil)
}

func (a *Api) getGroups(number string) ([]GroupEntry, error) {
	groupEntries := []GroupEntry{}

	message, err := a.s.ListGroups(number)
	if err != nil {
		return groupEntries, err
	}

	for _, group := range message.Data.Groups {
		var groupEntry GroupEntry

		groupEntry.InternalID = group.GroupID
		groupEntry.ID = convertInternalGroupIDToGroupID(groupEntry.InternalID)
		groupEntry.Name = group.Name

		groupEntry.Active = false
		for _, m := range group.Members {
			groupEntry.Members = append(groupEntry.Members, m.Number)
			if number == m.Number {
				groupEntry.Active = true
			}
		}

		groupEntry.Blocked = false

		groupEntries = append(groupEntries, groupEntry)
	}

	return groupEntries, nil
}

type Api struct {
	attachmentTmpDir string
	s                *signald.Signald
}

func NewApi(signaldSocketPath string, attachmentTmpDir string) *Api {
	return &Api{
		attachmentTmpDir: attachmentTmpDir,
		s: &signald.Signald{
			SocketPath: signaldSocketPath,
			Verbose:    false,
			StatusJSON: true,
		},
	}
}

// @Summary Lists general information about the API
// @Tags General
// @Description Returns the supported API versions and the internal build nr
// @Produce  json
// @Success 200 {object} About
// @Router /v1/about [get]
func (a *Api) About(c *gin.Context) {

	about := About{SupportedAPIVersions: []string{"v1", "v2"}, BuildNr: 2}
	c.JSON(200, about)
}

// @Summary Register a phone number.
// @Tags Devices
// @Description Register a phone number with the signal network.
// @Accept  json
// @Produce  json
// @Success 201
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Router /v1/register/{number} [post]
func (a *Api) RegisterNumber(c *gin.Context) {
	number := c.Param("number")

	var req RegisterNumberRequest

	buf := new(bytes.Buffer)
	buf.ReadFrom(c.Request.Body)
	if buf.String() != "" {
		err := json.Unmarshal(buf.Bytes(), &req)
		if err != nil {
			log.Error("Couldn't register number: ", err.Error())
			c.JSON(400, Error{Msg: "Couldn't process request - invalid request."})
			return
		}
	} else {
		req.UseVoice = false
	}

	if number == "" {
		c.JSON(400, gin.H{"error": "Please provide a number"})
		return
	}

	if _, err := a.s.Register(number, "", req.UseVoice); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, nil)
}

// @Summary Verify a registered phone number.
// @Tags Devices
// @Description Verify a registered phone number with the signal network.
// @Accept  json
// @Produce  json
// @Success 201 {string} string "OK"
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Param data body VerifyNumberSettings true "Additional Settings"
// @Param token path string true "Verification Code"
// @Router /v1/register/{number}/verify/{token} [post]
func (a *Api) VerifyRegisteredNumber(c *gin.Context) {
	number := c.Param("number")
	token := c.Param("token")

	pin := ""
	var req VerifyNumberSettings
	buf := new(bytes.Buffer)
	buf.ReadFrom(c.Request.Body)
	if buf.String() != "" {
		err := json.Unmarshal(buf.Bytes(), &req)
		if err != nil {
			log.Error("Couldn't verify number: ", err.Error())
			c.JSON(400, Error{Msg: "Couldn't process request - invalid request."})
			return
		}
		pin = req.Pin
	}

	if number == "" {
		c.JSON(400, gin.H{"error": "Please provide a number"})
		return
	}

	if token == "" {
		c.JSON(400, gin.H{"error": "Please provide a verification code"})
		return
	}

	if _, err := a.s.Verify(number, token, pin); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, nil)
}

// @Summary Send a signal message.
// @Tags Messages
// @Description Send a signal message
// @Accept  json
// @Produce  json
// @Success 201 {string} string "OK"
// @Failure 400 {object} Error
// @Param data body SendMessageV1 true "Input Data"
// @Router /v1/send [post]
// @Deprecated
func (a *Api) Send(c *gin.Context) {

	var req SendMessageV1
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(400, gin.H{"error": "Couldn't process request - invalid request"})
		return
	}

	base64Attachments := []string{}
	if req.Base64Attachment != "" {
		base64Attachments = append(base64Attachments, req.Base64Attachment)
	}

	a.send(c, req.Number, req.Message, req.Recipients, base64Attachments, req.IsGroup)
}

// @Summary Send a signal message.
// @Tags Messages
// @Description Send a signal message
// @Accept  json
// @Produce  json
// @Success 201 {string} string "OK"
// @Failure 400 {object} Error
// @Param data body SendMessageV2 true "Input Data"
// @Router /v2/send [post]
func (a *Api) SendV2(c *gin.Context) {
	var req SendMessageV2
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(400, gin.H{"error": "Couldn't process request - invalid request"})
		log.Error(err.Error())
		return
	}

	if len(req.Recipients) == 0 {
		c.JSON(400, gin.H{"error": "Couldn't process request - please provide at least one recipient"})
		return
	}

	groups := []string{}
	recipients := []string{}

	for _, recipient := range req.Recipients {
		if strings.HasPrefix(recipient, groupPrefix) {
			groups = append(groups, strings.TrimPrefix(recipient, groupPrefix))
		} else {
			recipients = append(recipients, recipient)
		}
	}

	if len(recipients) > 0 && len(groups) > 0 {
		c.JSON(400, gin.H{"error": "Signal Messenger Groups and phone numbers cannot be specified together in one request! Please split them up into multiple REST API calls."})
		return
	}

	// if len(groups) > 1 {
	// 	c.JSON(400, gin.H{"error": "A signal message cannot be sent to more than one group at once! Please use multiple REST API calls for that."})
	// 	return
	// }

	for _, group := range groups {
		a.send(c, req.Number, req.Message, []string{group}, req.Base64Attachments, true)
	}

	if len(recipients) > 0 {
		a.send(c, req.Number, req.Message, recipients, req.Base64Attachments, false)
	}
}

// @Summary Receive Signal Messages.
// @Tags Messages
// @Description Receives Signal Messages from the Signal Network.
// @Accept  json
// @Produce  json
// @Success 200 {object} []string
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Router /v1/receive/{number} [get]
func (a *Api) Receive(c *gin.Context) {
	number := c.Param("number")

	rc := make(chan signald.RawResponse)
	sc := make(chan struct{})
	a.s.Receive(rc, sc, number, 1, true)

	message := signald.RawResponse{}
	for {
		message = <-rc

		if message.Done {
			break
		}
	}

	c.JSON(200, message)
}

// @Summary Create a new Signal Group.
// @Tags Groups
// @Description Create a new Signal Group with the specified members.
// @Accept  json
// @Produce  json
// @Success 201 {object} CreateGroup
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Router /v1/groups/{number} [post]
func (a *Api) CreateGroup(c *gin.Context) {
	number := c.Param("number")

	type Request struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}

	var req Request
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(400, gin.H{"error": "Couldn't process request - invalid request"})
		log.Error(err.Error())
		return
	}

	if _, err := a.s.CreateGroup(number, "", req.Name, req.Members, ""); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	message, err := a.s.ListGroups(number)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	internalGroupID := ""
	for _, group := range message.Data.Groups {
		if group.Name == req.Name {
			internalGroupID = group.GroupID
			break
		}
	}

	c.JSON(201, CreateGroup{ID: convertInternalGroupIDToGroupID(internalGroupID)})
}

// @Summary List all Signal Groups.
// @Tags Groups
// @Description List all Signal Groups.
// @Accept  json
// @Produce  json
// @Success 200 {object} []GroupEntry
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Router /v1/groups/{number} [get]
func (a *Api) GetGroups(c *gin.Context) {
	number := c.Param("number")

	groups, err := a.getGroups(number)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, groups)
}

// @Summary Delete a Signal Group.
// @Tags Groups
// @Description Delete a Signal Group.
// @Accept  json
// @Produce  json
// @Success 200 {string} string "OK"
// @Failure 400 {object} Error
// @Param number path string true "Registered Phone Number"
// @Param groupid path string true "Group Id"
// @Router /v1/groups/{number}/{groupid} [delete]
func (a *Api) DeleteGroup(c *gin.Context) {
	base64EncodedGroupID := c.Param("groupid")
	number := c.Param("number")

	if base64EncodedGroupID == "" {
		c.JSON(400, gin.H{"error": "Please specify a group id"})
		return
	}

	groupID, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(base64EncodedGroupID, groupPrefix))
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid group id"})
		return
	}

	if _, err := a.s.LeaveGroup(number, base64.StdEncoding.EncodeToString(groupID)); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, nil)
}

// @Summary Link device and generate QR code.
// @Tags Devices
// @Description test
// @Produce  json
// @Success 200 {string} string	"Image"
// @Router /v1/link [get]
func (a *Api) Link(c *gin.Context) {
	deviceName := c.Query("device_name")

	if deviceName == "" {
		c.JSON(400, gin.H{"error": "Please provide a name for the device"})
		return
	}

	// We need to handle the socket connection so it stays up between function
	// calls.
	if !a.s.IsConnected() {
		if err := a.s.Connect(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
	}

	// First we call Link which returns the URI.
	message, err := a.s.Link(deviceName, "")
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		a.s.Disconnect()
		return
	}

	q, err := qrcode.New(message.Data.URI, qrcode.Medium)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		a.s.Disconnect()
		return
	}

	q.DisableBorder = true
	var png []byte
	png, err = q.PNG(256)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		a.s.Disconnect()
		return
	}

	// display the QRcode
	c.Data(200, "image/png", png)

	// call Link a second time with the returned request ID to get the status
	// of the link attempt.
	go func() {
		a.s.Link(deviceName, message.ID)
		a.s.Disconnect()
	}()
}
