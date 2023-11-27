package postgres

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"time"

	"github.com/pkg/errors"
	"github.com/wal-g/tracelog"
	"github.com/wal-g/wal-g/internal"
	"github.com/wal-g/wal-g/internal/daemon"
	"github.com/wal-g/wal-g/pkg/storages/storage"
	"github.com/wal-g/wal-g/utility"
)

const (
	SdNotifyWatchdog = "WATCHDOG=1"
)

type SocketWriteFailedError struct {
	error
}

func newSocketWriteFailedError(socketError error) SocketWriteFailedError {
	return SocketWriteFailedError{errors.Errorf("socket write failed: %v", socketError)}
}

func (err SocketWriteFailedError) Error() string {
	return fmt.Sprintf(tracelog.GetErrorFormatter(), err.error)
}

type DaemonOptions struct {
	SocketPath string
}

type SocketMessageHandler interface {
	Handle(ctx context.Context, messageBody []byte) error
}

type CheckMessageHandler struct {
	fd net.Conn
}

func (h *CheckMessageHandler) Handle(_ context.Context, _ []byte) error {
	_, err := h.fd.Write(daemon.OkType.ToBytes())
	if err != nil {
		return newSocketWriteFailedError(err)
	}
	tracelog.DebugLogger.Println("configuration successfully checked")
	return nil
}

type ArchiveMessageHandler struct {
	fd       net.Conn
	uploader *WalUploader
}

func (h *ArchiveMessageHandler) Handle(ctx context.Context, messageBody []byte) error {
	walFileName := string(messageBody)

	tracelog.DebugLogger.Printf("wal file name: %s\n", walFileName)

	fullPath, err := getFullPath(path.Join("pg_wal", walFileName))
	if err != nil {
		return err
	}
	tracelog.DebugLogger.Printf("starting wal-push: %s\n", fullPath)
	pushTimeout, err := internal.GetDurationSetting(internal.PgDaemonWALUploadTimeout)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	err = HandleWALPush(ctx, h.uploader, fullPath)
	if err != nil {
		return fmt.Errorf("file archiving failed: %w", err)
	}
	_, err = h.fd.Write(daemon.OkType.ToBytes())
	if err != nil {
		return newSocketWriteFailedError(err)
	}
	return nil
}

type WalFetchMessageHandler struct {
	fd     net.Conn
	reader internal.StorageFolderReader
}

func (h *WalFetchMessageHandler) Handle(_ context.Context, messageBody []byte) error {
	args, err := daemon.BytesToArgs(messageBody)
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return fmt.Errorf("wal-fetch incorrect arguments count")
	}
	walFileName := args[0]
	location := args[1]
	fullPath, err := getFullPath(location)
	if err != nil {
		return err
	}
	tracelog.DebugLogger.Printf("starting wal-fetch: %v -> %v\n", args[0], fullPath)

	err = HandleWALFetch(h.reader, walFileName, fullPath, DaemonPrefetcher{})
	if _, isArchNonExistErr := err.(internal.ArchiveNonExistenceError); isArchNonExistErr {
		tracelog.WarningLogger.Printf("ArchiveNonExistenceError: %v\n", err.Error())
		_, err = h.fd.Write(daemon.ArchiveNonExistenceType.ToBytes())
		if err != nil {
			return newSocketWriteFailedError(err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("WAL fetch failed: %w", err)
	}
	_, err = h.fd.Write(daemon.OkType.ToBytes())
	if err != nil {
		return newSocketWriteFailedError(err)
	}
	tracelog.DebugLogger.Printf("successfully fetched: %v -> %v\n", args[0], fullPath)
	return nil
}

func NewMessageHandler(
	messageType daemon.SocketMessageType,
	c net.Conn,
	storage storage.Storage,
) (SocketMessageHandler, error) {
	switch messageType {
	case daemon.CheckType:
		return &CheckMessageHandler{c}, nil
	case daemon.WalPushType:
		walUploader, err := PrepareMultiStorageWalUploader(storage.RootFolder(), "")
		if err != nil {
			return nil, err
		}
		return &ArchiveMessageHandler{c, walUploader}, nil
	case daemon.WalFetchType:
		folderReader, err := internal.PrepareMultiStorageFolderReader(storage.RootFolder(), "")
		if err != nil {
			return nil, err
		}

		return &WalFetchMessageHandler{c, folderReader}, nil
	default:
		return nil, nil
	}
}

type SocketMessageReader struct {
	c net.Conn
}

func NewMessageReader(c net.Conn) *SocketMessageReader {
	return &SocketMessageReader{c}
}

// Next method reads messages sequentially from the Reader
func (r SocketMessageReader) Next() (messageType daemon.SocketMessageType, messageBody []byte, err error) {
	messageParameters := make([]byte, 3)
	_, err = io.ReadFull(r.c, messageParameters)
	if err != nil {
		return daemon.ErrorType, nil, fmt.Errorf("failed to read params: %w", err)
	}
	messageType = daemon.SocketMessageType(messageParameters[0])
	var messageLength uint16
	l := bytes.NewReader(messageParameters[1:3])
	err = binary.Read(l, binary.BigEndian, &messageLength)
	if err != nil {
		return daemon.ErrorType, nil, fmt.Errorf("fail to read message len: %w", err)
	}
	messageBody = make([]byte, messageLength-3)
	_, err = io.ReadFull(r.c, messageBody)
	if err != nil {
		return daemon.ErrorType, nil, fmt.Errorf("failed to read msg body: %w", err)
	}
	return messageType, messageBody, err
}

// HandleDaemon is invoked to perform daemon mode
func HandleDaemon(options DaemonOptions) {
	if _, err := os.Stat(options.SocketPath); err == nil {
		err = os.Remove(options.SocketPath)
		if err != nil {
			tracelog.ErrorLogger.Fatal("Failed to remove socket file:", err)
		}
	}
	l, err := net.Listen("unix", options.SocketPath)
	if err != nil {
		tracelog.ErrorLogger.Fatal("Error on listening socket:", err)
	}

	sdNotifyTicker := time.NewTicker(30 * time.Second)
	defer sdNotifyTicker.Stop()
	go SendSdNotify(sdNotifyTicker.C)

	for {
		fd, err := l.Accept()
		if err != nil {
			tracelog.ErrorLogger.Fatal("Failed to accept, err:", err)
		}
		go Listen(context.Background(), fd)
	}
}

// Listen is used for listening connection and processing messages
func Listen(ctx context.Context, c net.Conn) {
	defer utility.LoggedClose(c, fmt.Sprintf("Failed to close connection with %s \n", c.RemoteAddr()))
	messageReader := NewMessageReader(c)
	for {
		messageType, messageBody, err := messageReader.Next()
		if err != nil {
			failAndLogError(c, fmt.Errorf("read message from %s, err: %v", c.RemoteAddr(), err))
			return
		}
		err = handleMessage(ctx, messageType, messageBody, c)
		if err != nil {
			failAndLogError(c, err)
			return
		}
		if messageType == daemon.WalPushType {
			tracelog.DebugLogger.Printf("successfully archived: %s\n", string(messageBody))
			return
		}
		if messageType == daemon.WalFetchType {
			tracelog.DebugLogger.Printf("successfully fetched: %s\n", string(messageBody))
			return
		}
	}
}

func handleMessage(
	ctx context.Context,
	messageType daemon.SocketMessageType,
	messageBody []byte,
	conn net.Conn,
) error {
	multiSt, err := ConfigureMultiStorage(true)
	defer utility.LoggedClose(multiSt, "close multi-storage")
	if err != nil {
		return fmt.Errorf("configure multi-storage: %w", err)
	}
	messageHandler, err := NewMessageHandler(messageType, conn, multiSt)
	if err != nil {
		return fmt.Errorf("init handler for message type %s: %v", string(messageType), err)
	}
	if messageHandler == nil {
		return fmt.Errorf("unexpected message type: %s", string(messageType))
	}
	err = messageHandler.Handle(ctx, messageBody)
	if err != nil {
		return fmt.Errorf("handle message: %w", err)
	}
	return nil
}

func failAndLogError(c net.Conn, err error) {
	tracelog.ErrorLogger.Printf("Message loop failure: %v", err)
	_, err = c.Write(daemon.ErrorType.ToBytes())
	if err != nil {
		tracelog.ErrorLogger.Printf("Sending error response failed: %v", err)
	}
}

func SendSdNotify(c <-chan time.Time) {
	for {
		<-c
		tracelog.ErrorLogger.PrintOnError(SdNotify(SdNotifyWatchdog))
	}
}

func SdNotify(state string) error {
	socketName, ok := internal.GetSetting(internal.SystemdNotifySocket)
	if !ok {
		return nil
	}
	socketAddr := &net.UnixAddr{
		Name: socketName,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	if err != nil {
		return fmt.Errorf("failed connect to service: %w", err)
	}
	defer utility.LoggedClose(conn, fmt.Sprintf("Failed to close connection with %s \n", conn.RemoteAddr()))
	if _, err = conn.Write([]byte(state)); err != nil {
		return newSocketWriteFailedError(err)
	}
	return nil
}

func getFullPath(relativePath string) (string, error) {
	PgDataSettingString, ok := internal.GetSetting(internal.PgDataSetting)
	if !ok {
		return "", fmt.Errorf("PGDATA is not set in the conf")
	}
	return path.Join(PgDataSettingString, relativePath), nil
}
