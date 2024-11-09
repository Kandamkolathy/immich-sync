package main

import (
	"flag"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kandamkolathy/immich-sync/client"
	"github.com/Kandamkolathy/immich-sync/utilities"
	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/spf13/viper"
)

var logger service.Logger

// Program structures.
//
//	Define Start and Stop methods.
type program struct {
	exit chan struct{}
}

type workQueue struct {
	connected       bool
	updateConnected chan bool
	fileBuf         []string
}

func (p *program) Start(s service.Service) error {
	if service.Interactive() {
		logger.Info("Running in terminal.")
	} else {
		logger.Info("Running under service manager.")
	}
	p.exit = make(chan struct{})

	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (w *workQueue) handleFileCreate(event fsnotify.Event, immichClient client.ImmichClient) {
	logger.Info("modified file:", event.Name)
	ext := strings.ToLower(filepath.Ext(event.Name))

	if immichClient.IsExtensionSupported(ext) {
		if !w.connected {
			w.fileBuf = append(w.fileBuf, event.Name)
			return
		}
		res, err := immichClient.UploadImage(event.Name)
		// Check for HTTP error
		if err != nil {
			logger.Error(err)
			err := immichClient.CheckConnectivty()
			if err != nil {
				// Set value in immich client that will trigger intermittent checks for restablishing connectivity
				// Until then push to buffer
				logger.Info("Connectivity lost, storing to buffer and retrying")
				w.connected = false
				w.fileBuf = append(w.fileBuf, event.Name)
				go immichClient.WaitForConnectivity(w.updateConnected)
			}
		}
		logger.Info(string(res))
	}
}

func (w *workQueue) handleReconnect(immichClient client.ImmichClient) {
	w.connected = true
	//capture and handle errors for bulk upload
	logger.Info("Connectivity restablished, uploading buffer")
	err := immichClient.BulkUpload(w.fileBuf)

	//Manage this error situation better
	if err != nil {
		logger.Error(err)
		err := immichClient.CheckConnectivty()
		if err != nil {
			// Set value in immich client that will trigger intermittent checks for restablishing connectivity
			// Until then push to buffer
			logger.Info("Connectivity lost, storing to buffer and retrying")
			w.connected = false
			go immichClient.WaitForConnectivity(w.updateConnected)
			return
		} else {
			err := immichClient.BulkUpload(w.fileBuf)
			if err != nil {
				logger.Error(err)
			}
		}
	}
	w.fileBuf = []string{}
}

func (p *program) run() error {
	logger.Infof("I'm running %v.", service.Platform())

	immichClient, _ := client.NewImmichClient(logger)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error(err)
	}
	defer watcher.Close()

	w := workQueue{connected: true, fileBuf: make([]string, 0), updateConnected: make(chan bool)}

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				logger.Info("event:", event)
				if event.Has(fsnotify.Create) {
					w.handleFileCreate(event, immichClient)
				}
			case update, ok := <-w.updateConnected:
				logger.Info("Connectivity update")
				if !ok {
					return
				}
				if update == true {
					w.handleReconnect(immichClient)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Error("error:", err)
			}
		}
	}()

	shaMap := make([]client.ChecksumPair, 0)

	// Add paths.
	for _, path := range viper.GetStringSlice("paths") {
		err = watcher.Add(path)
		if err != nil {
			logger.Error(err)
			return err
		}

		filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				err := watcher.Add(path)
				logger.Info(path)
				if err != nil {
					logger.Error(err)
					return err
				}
			} else if !d.IsDir() && immichClient.IsExtensionSupported(filepath.Ext(path)) {
				return utilities.GetFileSHAs(&shaMap, path, immichClient.Logger)
			}

			return nil
		})
	}

	logger.Info("Syncing existing images in path")
	checkData, err := immichClient.GetNewFiles(shaMap)
	if err != nil {
		logger.Info(err)
	}

	for _, image := range checkData.Results {
		if image.Action == "accept" {
			res, err := immichClient.UploadImage(image.ID)
			if err != nil {
				logger.Error(err)
			}
			logger.Info(string(res))
		}
	}

	logger.Info("Finished syncing existing images in paths")

	// Block main goroutine forever.
	<-make(chan struct{})

	return err
}
func (p *program) Stop(s service.Service) error {
	// Any work in Stop should be quick, usually a few seconds at most.
	logger.Info("I'm Stopping!")
	close(p.exit)
	return nil
}

func setupLogger(s service.Service) {
	errs := make(chan error, 5)
	var err error
	logger, err = s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()
}

// Service setup.
//
//	Define service config.
//	Create the service.
//	Setup the logger.
//	Handle service controls (optional).
//	Run the service.
func main() {
	var paths utilities.ArrayFlags
	svcFlag := flag.String("service", "", "Control the system service.")
	serverURL := flag.String("server", "", "URL For immich server to make api calls.")
	flag.Var(&paths, "path", "Add path to folder to sync.")
	key := flag.String("key", "", "API Key to server.")

	flag.Parse()

	options := make(service.KeyValue)
	options["Restart"] = "on-success"
	options["SuccessExitStatus"] = "1 2 8 SIGKILL"
	options["UserService"] = true
	options["LogOutput"] = true
	options["LogDirectory"] = "/tmp"
	svcConfig := &service.Config{
		Name:         "immich-sync",
		DisplayName:  "Immich Sync Service",
		Description:  "Service that syncs images to an Immich Server.",
		Dependencies: []string{},
		Option:       options,
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	setupLogger(s)

	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			logger.Infof("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}

	createConfig(serverURL, key, paths)

	err = s.Run()
	if err != nil {
		logger.Info(err)
	}
}

func createConfig(serverURL *string, key *string, paths utilities.ArrayFlags) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		logger.Info(err)
	}
	if !utilities.FileExists(configDir + "/immich-sync/config.yaml") {
		err := os.Mkdir(configDir+"/immich-sync", 0755)
		if err != nil {
			logger.Info(err)
		}
		_, err = os.Create(configDir + "/immich-sync/config.yaml")
		if err != nil {
			logger.Info(err)
		}
	}
	viper.AddConfigPath(configDir + "/immich-sync")
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	err = viper.ReadInConfig()
	if err != nil {
		logger.Info(err)
	}

	if *serverURL != "" {
		viper.Set("server", *serverURL)
	}

	if len(paths) != 0 {
		viper.Set("paths", []string(paths))
	}

	if *key != "" {
		viper.Set("key", *key)
	}

	if viper.GetString("server") == "" || len(viper.GetStringSlice("paths")) == 0 || viper.GetString("key") == "" {
		logger.Info("Server, paths, or key not configured")
		return
	}

	err = viper.WriteConfigAs(configDir + "/immich-sync/config.yaml")
	if err != nil {
		logger.Info(err)
	}

	viper.OnConfigChange(func(e fsnotify.Event) {
		logger.Info("Config file changed:", e.Name)
	})
	viper.WatchConfig()
}
