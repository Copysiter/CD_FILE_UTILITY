package main

import (
	"context"
	"errors"
	"flag"
	"fts-cd-file-utility/cfg"
	"fts-cd-file-utility/common"
	"fts-cd-file-utility/deliver"
	"fts-cd-file-utility/deploy"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	configFlag := flag.String("config", "", "")
	flag.Parse()

	initConfig, err := cfg.ReadInitConfig(*configFlag)
	if err != nil {
		log.Println("failed to read config")
		os.Exit(1)
	}
	//log.Println(initConfig)
	setupConfig(*initConfig)
	/*
		jobFilePath := "D:\\tmp\\pypi\\20241028094027.job"
		jobFile, err := os.Open(jobFilePath)
		jobFileContent, err := io.ReadAll(jobFile)
		if deploy.IsDockerArtifact(jobFileContent) {
			log.Println("docker artifact upload job found")
		} else if deploy.IsPypiArtifact(jobFileContent) {
			log.Println("pypi artifact upload job found")

		}*/
	runApp()
}

func setupConfig(config cfg.StartupConfig) {
	config.RefineConfig()
	common.StartupConfig = config
}

func runApp() {
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Routes
	e.GET("/", common.ReadConfig)

	if common.StartupConfig.Mode == cfg.CdSendMode {
		e.GET("/cd-ping/:jobId", deliver.GetJobStatus)
		e.GET("/cd-ping/latest", deliver.GetLatestJobStatus)
		//e.POST("/cd-start/:jobId", deliver.StartFileCdHandler)
		e.POST("/cd-pypi-start", deliver.StartPypiCdHandler)
		e.POST("/cd-hf-start", deliver.StartHfCdHandler)

		if common.StartupConfig.SendDockerEnabled {
			common.InitDockerClientApiVersion()
			e.POST("/cd-docker-start/:jobId", deliver.StartDockerCdHandlerWithJobId)
			e.POST("/cd-docker-start", deliver.StartDockerCdHandler)
		} else {
			log.Println("docker artifacts won't be sent since property `send_docker_enabled` set to false")
		}
		// start goroutine that will move jobs from status DOWNLOADING_DONE to SUCCESS
		go deliver.CheckDownloadingDoneJobs()

		go deliver.DeleteStaleJobs()
	} else if common.StartupConfig.Mode == cfg.CdReceiveMode {
		if common.StartupConfig.ReceivePypiEnabled {
			if !common.IsTwineInstalled() {
				log.Fatalln("Twine is not installed. Can't proceed\nInstall Twine or set `receive_pypi_enabled` property to true in receive-config.json")
			}
		} else {
			log.Println("pypi artifact won't be processed since property `receive_pypi_enabled` set to false")
		}

		if common.StartupConfig.ReceiveDockerEnabled {
			common.InitDockerClientApiVersion()
			e.POST("/cd-docker-deploy/:jobId", deploy.StartDockerDeployHandler)
		} else {
			log.Println("docker artifacts won't be processed since property `receive_docker_enabled` set to false")
		}
		go deploy.LoadArtifacts(ctx, &common.StartupConfig)
	} else {
		log.Fatalln("invalid mode set", common.StartupConfig.Mode)
	}

	e.GET("/check-nfs-read", common.CheckNfsStorageForReading)
	e.GET("/check-nfs-write", common.CheckNfsStorageForWriting)

	// Start server
	go func() {
		// e.Logger.Fatal(e.Start(common.StartupConfig.StartupPort))
		if err := e.Start(common.StartupConfig.StartupPort); err != nil && !errors.Is(err, http.ErrServerClosed) {
			e.Logger.Fatal("shutting down the server")
		}
	}()

	// graceful shutdown
	<-ctx.Done()
	log.Println("Shutting down server: setting 10s timeout!")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatal(err)
	}
}
