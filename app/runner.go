package app

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/timemore/foundation/logger"
)

var log = logger.NewPkgLogger()

func RunServers(servers []ServiceServer) {
	if len(servers) == 0 {
		return
	}

	// Used to determine if all servers have stopped.
	var serversStopWaiter sync.WaitGroup

	// Start the servers
	for _, srv := range servers {
		serversStopWaiter.Add(1)
		go func(innerSrv ServiceServer) {
			srvName := innerSrv.ServerName()
			log.Info().Msgf("Starting %s...", srvName)
			err := innerSrv.Serve()
			if err != nil {
				log.Fatal().Err(err).Msgf("%s serve", srvName)
			} else {
				log.Info().Msgf("%s stopped", srvName)
			}
			serversStopWaiter.Done()
		}(srv)
	}

	// We set up the signal handler (interrupt and terminate).
	// We are using the signal to gracefully and forcefully stop the server.
	shutdownSignal := make(chan os.Signal)
	// Listen to interrupt and terminal signals
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	// Wait for the shutdown signal
	<-shutdownSignal

	log.Info().Msg("Shutting down servers...")

	// Start another routine to catch another signal so the shutdown
	// could be forced. If we get another signal, we'll exit immediately
	go func() {
		<-shutdownSignal
		log.Info().Msg("Forced shutdown.")
		os.Exit(0)
	}()

	// Gracefully shutdown the servers
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, srv := range servers {
		go func(innerSrv ServiceServer) {
			srvName := innerSrv.ServerName()
			log.Info().Msgf("shutting down %s...", srvName)
			err := innerSrv.Shutdown(shutdownCtx)
			if err != nil {
				log.Err(err).Msgf("%s shutdown", srvName)
			}
		}(srv)
	}

	// Wait for all servers to stop.
	serversStopWaiter.Wait()

	log.Info().Msg("Done.")
}
