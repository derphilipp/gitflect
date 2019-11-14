package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"log"
	"os"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
	"gopkg.in/yaml.v3"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

const githubURL = "https://github.com/"

var (
	configFile = flag.String("config-file", "config.yaml", "path to custom configuration file")
	// mazeFile   = flag.String("maze-file", "maze01.txt", "path to a custom maze file")
)

const (
	statusDone        = "✅ project updated"
	statusDoneNothing = "✅ already up to date"
	statusDownload    = "⏬ downloading"
	statusUpload      = "⏫ uploading"
	statusError       = "🚨 error"
	statusUnknown     = "❓unknown"
	statusWaiting     = "⌛ waiting"
)

type Project struct {
	LocalName string `yaml:"local_name"`
	OriginURL string `yaml:"origin_url"`
}

type Config struct {
	DefaultURL     string             `yaml:"default_url"`
	DataPath       string             `yaml:"data_path"`
	SSHKey         string             `yaml:"ssh_key"`
	Parallel       bool               `yaml:"parallel"`
	GithubProjects []string           `yaml:"github_projects"`
	Projects       map[string]Project `yaml:"projects"`
}

var PublicKey *ssh.PublicKeys
var cfg Config

func loadConfig() error {

	f, err := os.Open(*configFile)
	if err != nil {
		return err
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		return err
	}

	sshFile, err := ioutil.ReadFile(cfg.SSHKey)
	if err != nil {
		log.Printf("Could not load %s:\n", cfg.SSHKey)
		return err
	}
	publicKey, err := ssh.NewPublicKeys("git", []byte(sshFile), "")
	if err != nil {
		log.Println(err)
		return err
	}
	PublicKey = publicKey

	return nil
}

func clone(url, localPath string, reponame string) error {
	updateState(statusDownload, reponame)
	//	null, _ := os.Open(os.DevNull)
	err := os.MkdirAll(localPath, os.ModePerm)
	if err == os.ErrExist {
	} else if err != nil {
		addOutputLine("Could not create directory", reponame)
		updateState(statusError, reponame)
		return err
	}
	// Clone
	_, err = git.PlainClone(localPath, true, &git.CloneOptions{
		URL:      url,
		Progress: nil,
	})

	if err == git.ErrRepositoryAlreadyExists {
		addOutputLine("Repo already exists, nice...", reponame)
		//		log.Printf("Repo already exists, nice...")
	} else if err != nil {
		addOutputLine(fmt.Sprintf("Cloning Failed: %v", err), reponame)
		updateState(statusError, reponame)
		//		log.Printf("Cloning failed: %v\n", err)
		return err
	}
	return nil
}

func loadRepo(path string, reponame string) (*git.Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		addOutputLine(fmt.Sprintf("Loading repository failed: %v\n", err), reponame)
		updateState(statusError, reponame)
		return nil, err
	}
	return repo, nil
}

func fetch(repo *git.Repository, reponame string) error {
	// Fetch
	updateState(statusDownload, reponame)
	err := repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Force:      true,
		Tags:       git.AllTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/heads/*"),
			config.RefSpec("+refs/pull/*/head:refs/pull/*/head"),
		},
	})
	if err == git.NoErrAlreadyUpToDate {
		addOutputLine(fmt.Sprintf("Already up to date"), reponame)
		//		log.Printf("Already up to date")
		return nil
	} else if err != nil {
		addOutputLine(fmt.Sprintf("Fetching existing repo failed: %v\n", err), reponame)
		updateState(statusError, reponame)
		return err
	}
	return nil
}

func setRemote(repo *git.Repository, targetURL string) error {
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "gittig",
		URLs: []string{targetURL}})
	return err
}

func push(repo *git.Repository, reponame string) error {
	// Push
	err := repo.Push(&git.PushOptions{
		RemoteName: "gittig",
		Prune:      true,
		Auth:       PublicKey,
	})

	if err == git.NoErrAlreadyUpToDate {
		updateState(statusDoneNothing, reponame)
		return nil
	} else if err == nil {
		updateState(statusDone, reponame)
	}
	updateState(statusError, reponame)
	addOutputLine(fmt.Sprintf("Error occured: [%v](fg:red)", err), reponame)
	return err
}

func processProject(url, localname string) error {

	addOutputLine("Processing Project", localname)

	targetURL := cfg.DefaultURL + localname
	projectPath := cfg.DataPath + localname
	updateState(statusWaiting, localname)
	clone(url, projectPath, localname)
	repo, err := loadRepo(projectPath, localname)
	if err != nil {
		updateState(statusError, localname)
		return errors.New("Loading Repo failed")
	}
	fetch(repo, localname)
	if err != nil {
		updateState(statusError, localname)
		return errors.New("Fetching Repo failed")
	}

	repo.DeleteRemote("gittig")
	setRemote(repo, targetURL)

	updateState(statusUpload, localname)
	err = push(repo, localname)
	return err
}

func startProcessing() {
	wg := sync.WaitGroup{}
	for projectkey := range cfg.Projects {
		project := cfg.Projects[projectkey]
		if cfg.Parallel {
			go processProject(project.OriginURL, project.LocalName)
			addActiveRepo(project.LocalName)
			wg.Add(1)
		} else {
			processProject(project.OriginURL, project.LocalName)
		}
	}

	for _, project := range cfg.GithubProjects {
		if cfg.Parallel {
			go processProject(githubURL+project, project)
			addActiveRepo(project)
			wg.Add(1)
		} else {
			processProject(githubURL+project, project)
		}
	}

}

var grid *ui.Grid
var repoList *widgets.Table
var textList *widgets.List

func addOutputLine(text, projectname string) {
	newEntry := fmt.Sprintf("[%s](fg:blue):\t %s", projectname, text)
	textList.Rows = append(textList.Rows, newEntry)
}

func addActiveRepo(projectname string) {
	newEntry := []string{projectname, "active"}
	repoList.Rows = append(repoList.Rows, newEntry)
}

func findRowIndex(text string, table *widgets.Table) int {
	for i, row := range table.Rows {
		if row[0] == text {
			return i
		}
	}
	return -1
}

func updateState(newstate, projectname string) {
	i := findRowIndex(projectname, repoList)
	if i != -1 {
		repoList.Rows[i][1] = newstate
	} else {
		repoList.Rows = append(repoList.Rows, []string{projectname, newstate})
	}
}

func updateGridEnv() {
	termWidth, termHeight := ui.TerminalDimensions()
	grid.SetRect(0, 0, termWidth, termHeight)
	grid.Set(
		ui.NewRow(1,
			ui.NewCol(1.0/3, repoList),
			ui.NewCol(2.0/2, textList),
		),
	)
}

func main() {
	flag.Parse()
	err := loadConfig()
	if err != nil {
		log.Printf("Error loading configuration: %v\n", err)
		return
	}
	if _, err := os.Stat(cfg.DataPath); os.IsNotExist(err) {
		log.Printf("Data directory does not exist.")
	}

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()
	uiEvents := ui.PollEvents()

	repoList = widgets.NewTable()
	repoList.Title = "Active Elements"
	repoList.Rows = [][]string{{"Repo", "Status"}}
	repoList.RowSeparator = false
	//	repoList.TextStyle = ui.NewStyle(ui.ColorYellow)
	repoList.SetRect(0, 0, 60, 75)

	textList = widgets.NewList()

	textList.Title = "Log output"
	textList.WrapText = true
	//textList.RowSeparator = false
	//textList.Rows = [][]string{{"Repo", "Text"}}
	//	repoList.TextStyle = ui.NewStyle(ui.ColorYellow)

	textList.SetRect(61, 0, 160, 75)

	grid = ui.NewGrid()

	drawFunction()
	//	ui.Render(repoList, textList)

	go startProcessing()

	ticker := time.NewTicker(time.Second).C

	for {
		select {
		case e := <-uiEvents:
			switch e.ID { // event string/identifier
			case "q", "<C-c>": // press 'q' or 'C-c' to quit
				return
				/*case "<MouseLeft>":
					payload := e.Payload.(ui.Mouse)
					x, y := payload.X, payload.Y
				case "<Resize>":
					payload := e.Payload.(ui.Resize)
					width, height := payload.Width, payload.Height
				}
				switch e.Type {
				case ui.KeyboardEvent: // handle all key presses
					eventID = e.ID // keypress string
				}*/
				// use Go's built-in tickers for updating and drawing data

			}
		case <-ticker:
			drawFunction()

		}
	}
}

func drawFunction() {
	updateGridEnv()
	if len(textList.Rows) > 0 {
		textList.ScrollBottom()
	}
	ui.Render(grid)
}
