// Package riotbot implements a Service for user onboarding in Riot.
package riotbot

import (
	"bytes"
	"io/ioutil"
	"log"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// Service represents the Riotbot service. It has no Config fields.
type Service struct {
	types.DefaultService
}

// TutorialFlow represents the tutorial flow / steps
type TutorialFlow struct {
	ResourcesBaseURL string            `yaml:"resources_base_url"`
	Templates        map[string]string `yaml:"templates"`
	InitialDelay     time.Duration     `yaml:"initial_delay"`
	Tutorial         struct {
		Steps []TutorialStep `yaml:"steps"`
	} `yaml:"tutorial"`
}

type TutorialStep struct {
	Type  string        `yaml:"type"`
	Body  string        `yaml:"body"`
	Src   string        `yaml:"src"`
	Delay time.Duration `yaml:"delay"`
}

// ServiceType of the Riotbot service
const ServiceType = "riotbot"

// "Tutorial flow structure
var tutorialFlow *TutorialFlow

// Tutorial instances
var tutorials []Tutorial

// Tutorial represents the current totorial instances
type Tutorial struct {
	roomID      string
	userID      string
	currentStep int
	timer       *time.Timer
	cli         *gomatrix.Client
	templates   map[string]string
}

// NewTutorial creates a new Tutorial instance
func NewTutorial(roomID string, userID string, cli *gomatrix.Client, templates map[string]string) Tutorial {
	t := Tutorial{
		roomID:      roomID,
		userID:      userID,
		currentStep: -1,
		timer:       nil,
		cli:         cli,
		templates:   templates,
	}
	return t
}

func (t *Tutorial) restart() {
	if t.timer != nil {
		t.timer.Stop()
	}
	t.currentStep = -1
	t.queueNextStep(tutorialFlow.InitialDelay)
}

func (t *Tutorial) queueNextStep(delay time.Duration) {
	if t.timer != nil {
		t.timer.Stop()
	}

	log.Printf("Queueing next step of tutorial for user %s (current step %d) to run in %dms", t.userID, t.currentStep, delay)
	if delay > 0 {
		t.timer = time.NewTimer(time.Millisecond * delay)
		<-t.timer.C
		t.nextStep()
	} else {
		t.nextStep()
	}
}

func (t Tutorial) nextStep() {
	t.currentStep++
	log.Printf("Performing next step (%d) of tutorial for %s", t.currentStep, t.userID)
	// Check that there is a valid mtutorial step to process
	if t.currentStep < len(tutorialFlow.Tutorial.Steps) {
		base := tutorialFlow.ResourcesBaseURL
		step := tutorialFlow.Tutorial.Steps[t.currentStep]
		// Check message type
		switch step.Type {
		case "image":
			body := t.renderBody(step)
			msg := gomatrix.ImageMessage{
				MsgType: "m.image",
				Body:    body,
				URL:     base + step.Src,
			}

			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Print("Failed to send Image message")
			} else {
				log.Printf("Seinding Image message - %s", body)
			}
		case "notice":
			body := t.renderBody(step)
			msg := gomatrix.TextMessage{
				MsgType: "m.notice",
				Body:    body,
			}
			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Printf("Failed to send Notice message - %s", body)
			} else {
				log.Printf("Seinding Notice message - %s", body)
			}
		default: // text
			body := t.renderBody(step)
			msg := gomatrix.TextMessage{
				MsgType: "m.text",
				Body:    body,
			}
			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Printf("Failed to send Text message - %s", body)
			} else {
				log.Printf("Seinding Text message - %s", body)
			}
		}

		// TODO -- If last step, clean up tutorial instance

		// Set up timer for next step
		t.queueNextStep(step.Delay)
	} else {
		log.Println("Tutorial instance ended")
		// End of tutorial -- TODO remove tutorial instance
	}
}

func (t Tutorial) renderBody(ts TutorialStep) string {
	if ts.Body != "" {
		tmpl, err := template.New("message").Parse(ts.Body)
		if err != nil {
			log.Print("Failed to create message template")
		}
		var msg bytes.Buffer
		if err = tmpl.Execute(&msg, t.templates); err != nil {
			log.Print("Failed to execute template substitution")
			return ""
		}
		return msg.String()
	}

	return ""
}

// Commands supported:
//    !start
// Starts the tutorial.
func (e *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"start"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				response := initTutorialFlow(cli, roomID, userID)
				return &gomatrix.TextMessage{MsgType: "m.notice", Body: response}, nil
			},
		},
	}
}

func initTutorialFlow(cli *gomatrix.Client, roomID string, userID string) string {
	// Check if there is an existing tutorial for this user and restart it, if found
	for t := range tutorials {
		tutorial := tutorials[t]
		if tutorial.userID == userID {
			tutorial.restart()
			log.Printf("Restarting Riot tutorial %d", t)
			return "Restarting Riot tutorial"
		}
	}
	log.Print("Existing tutorial instance not found for this user")

	// Start a new instance of the riot tutorial
	tutorial := NewTutorial(roomID, userID, cli, tutorialFlow.Templates)
	tutorials = append(tutorials, tutorial)
	go tutorial.queueNextStep(tutorialFlow.InitialDelay)
	log.Printf("Starting Riot tutorial: %v", tutorial)
	return "Starting Riot tutorial"
}

func getScriptPath() string {
	_, script, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("Failed to get script dir")
	}

	return filepath.Dir(script)
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})

	var tutorialFlowFileName = getScriptPath() + "/tutorial.yml"
	tutorialFlowYaml, err := ioutil.ReadFile(tutorialFlowFileName)
	if err != nil {
		log.Fatalf("Failed to read tutorial yaml config file (%s): %v ", tutorialFlowFileName, err)
	}
	if err = yaml.Unmarshal(tutorialFlowYaml, &tutorialFlow); err != nil {
		log.Fatalf("Failed to unmarshal tutorial config yaml: %v", err)
	}
}
