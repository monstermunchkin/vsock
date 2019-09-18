// +build !windows

package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/gorilla/websocket"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/termios"
	"golang.org/x/sys/unix"
)

func getTERM() (string, bool) {
	return os.LookupEnv("TERM")
}

func sendTermSize(control *websocket.Conn) error {
	width, height, err := termios.GetSize(unix.Stdout)
	if err != nil {
		return err
	}

	log.Printf("Window size is now: %dx%d\n", width, height)

	w, err := control.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}

	msg := api.ContainerExecControl{}
	msg.Command = "window-resize"
	msg.Args = make(map[string]string)
	msg.Args["width"] = strconv.Itoa(width)
	msg.Args["height"] = strconv.Itoa(height)

	buf, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)

	w.Close()
	return err
}

func controlSocketHandler(control *websocket.Conn) {
	ch := make(chan os.Signal, 10)
	signal.Notify(ch,
		unix.SIGWINCH,
		unix.SIGTERM,
		unix.SIGHUP,
		unix.SIGINT,
		unix.SIGQUIT,
		unix.SIGABRT,
		unix.SIGTSTP,
		unix.SIGTTIN,
		unix.SIGTTOU,
		unix.SIGUSR1,
		unix.SIGUSR2,
		unix.SIGSEGV,
		unix.SIGCONT)

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	defer control.WriteMessage(websocket.CloseMessage, closeMsg)

	for {
		sig := <-ch
		switch sig {
		case unix.SIGWINCH:
			log.Printf("Received '%s signal', updating window geometry.\n", sig)
			err := sendTermSize(control)
			if err != nil {
				log.Printf("error setting term size %s\n", err)
				return
			}
		case unix.SIGTERM:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGTERM)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGTERM)
				return
			}
		case unix.SIGHUP:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGHUP)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGHUP)
				return
			}
		case unix.SIGINT:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGINT)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGINT)
				return
			}
		case unix.SIGQUIT:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGQUIT)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGQUIT)
				return
			}
		case unix.SIGABRT:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGABRT)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGABRT)
				return
			}
		case unix.SIGTSTP:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGTSTP)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGTSTP)
				return
			}
		case unix.SIGTTIN:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGTTIN)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGTTIN)
				return
			}
		case unix.SIGTTOU:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGTTOU)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGTTOU)
				return
			}
		case unix.SIGUSR1:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGUSR1)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGUSR1)
				return
			}
		case unix.SIGUSR2:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGUSR2)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGUSR2)
				return
			}
		case unix.SIGSEGV:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGSEGV)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGSEGV)
				return
			}
		case unix.SIGCONT:
			log.Printf("Received '%s signal', forwarding to executing program.\n", sig)
			err := forwardSignal(control, unix.SIGCONT)
			if err != nil {
				log.Printf("Failed to forward signal '%s'.\n", unix.SIGCONT)
				return
			}
		default:
			break
		}
	}
}

func forwardSignal(control *websocket.Conn, sig unix.Signal) error {
	log.Printf("Forwarding signal: %s", sig)

	w, err := control.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}

	msg := api.ContainerExecControl{}
	msg.Command = "signal"
	msg.Signal = int(sig)

	buf, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)

	w.Close()
	return err
}
