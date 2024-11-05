package main

import (
	"crypto/sha1"
	"encoding/base64"
	"flag"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kandamkolathy/immich-sync/client"
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

func fileExists(filename string) bool {
	// Use os.Stat to get file information
	_, err := os.Stat(filename)
	// If there's no error, the file exists
	if err == nil {
		return true
	}
	// If the error is because the file does not exist, return false
	if os.IsNotExist(err) {
		return false
	}
	// If there's another kind of error, you can handle it as needed
	return false
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

func getFileSHAs(shaMap *[]client.ChecksumPair, path string) error {
	f, err := os.Open(path)
	if err != nil {
		logger.Info("Failed to open file for SHA")
		logger.Info(err)
		return err
	}

	defer f.Close()

	h := sha1.New()

	if _, err := io.Copy(h, f); err != nil {
		logger.Info("SHA byte copy failed")
		logger.Info(err)
		return err
	}
	*shaMap = append(*shaMap, client.ChecksumPair{Checksum: base64.StdEncoding.EncodeToString(h.Sum(nil)), ID: path})
	return nil
}

func isExtensionSupported(supportedTypes []string, ext string) bool {
	normalisedExt := strings.ToLower(ext)

	for _, t := range supportedTypes {
		if t == normalisedExt {
			return true
		}
	}

	return false
}

func startUp() (client.ImmichClient, client.SupportedMediaTypesResponse, error) {
	immichClient, err := client.NewImmichClient(logger)

	mediaTypes, err := immichClient.GetSupportedMediaTypes()
	if err != nil {
		logger.Error(err)
	}
	return immichClient, mediaTypes, nil
}

func (p *program) run() error {
	logger.Infof("I'm running %v.", service.Platform())
	immichClient, mediaTypes, err := startUp()
	if err != nil {
		logger.Error(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error(err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		fileBuf := make([]string, 0)
		connected := true
		updateConnected := make(chan bool)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				logger.Info("event:", event)
				if event.Has(fsnotify.Create) {
					logger.Info("modified file:", event.Name)
					ext := strings.ToLower(filepath.Ext(event.Name))

					if isExtensionSupported(mediaTypes.Image, ext) {
						if !connected {
							fileBuf = append(fileBuf, event.Name)
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
								connected = false
								fileBuf = append(fileBuf, event.Name)
								go immichClient.WaitForConnectivity(updateConnected)
							}
						}
						logger.Info(string(res))
					}
				}
			case update, ok := <-updateConnected:
				logger.Info("Connectivity update")
				if !ok {
					return
				}
				if update == true {
					connected = true
					//capture and handle errors for bulk upload
					logger.Info("Connectivity restablished, uploading buffer")
					err := immichClient.BulkUpload(fileBuf)
					//Manage this error situation better
					if err != nil {
						logger.Error(err)
					}
					fileBuf = []string{}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Error("error:", err)
			}
		}
	}()

	// Add a path.

	shaMap := make([]client.ChecksumPair, 0)

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

			ext := strings.ToLower(filepath.Ext(path))

			if d.IsDir() {
				err := watcher.Add(path)
				logger.Info(path)
				if err != nil {
					logger.Error(err)
					return err
				}
			} else if !d.IsDir() && isExtensionSupported(mediaTypes.Image, ext) {
				return getFileSHAs(&shaMap, path)
			}

			return nil
		})
	}

	logger.Info("Syncing existing images in path")
	checkData, err := immichClient.GetNewFiles(shaMap)
	if err != nil {
		logger.Info(err)
	}

	//logger.Info(checkData)

	for _, image := range checkData.Results {
		if image.Action == "accept" {
			res, err := immichClient.UploadImage(image.ID)
			if err != nil {
				logger.Error(err)
			}
			logger.Info(string(res))
		}
	}

	logger.Info("Finished syncing existing images in path")

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

// Created so that multiple inputs can be accecpted
type arrayFlags []string

func (i *arrayFlags) String() string {
	// change this, this is just can example to satisfy the interface
	return "my string representation"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, strings.TrimSpace(value))
	return nil
}

// Service setup.
//
//	Define service config.
//	Create the service.
//	Setup the logger.
//	Handle service controls (optional).
//	Run the service.
func main() {
	var paths arrayFlags
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
	errs := make(chan error, 5)
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

	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			logger.Infof("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}

	configDir, _ := os.UserConfigDir()
	if !fileExists(configDir + "/immich-sync/config.yaml") {
		if err != nil {
			logger.Info(err)
		}
		err = os.Mkdir(configDir+"/immich-sync", 0755)
		if err != nil {
			logger.Info(err)
		}
		os.Create(configDir + "/immich-sync/config.yaml")
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

	err = s.Run()
	if err != nil {
		logger.Info(err)
	}
}
