package service

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageCaptureBodyStaysInMemoryAndHashes(t *testing.T) {
	capture := NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 64, MemoryBudgetBytes: 1024, SpoolDirectory: t.TempDir()})
	_, err := capture.RequestWriter().Write([]byte(`{"hello":"world"}`))
	require.NoError(t, err)
	artifact := capture.Artifact()
	require.Equal(t, BodyStateAvailable, artifact.Request.State)
	require.Equal(t, int64(17), artifact.Request.RawBytes)
	want := sha256.Sum256([]byte(`{"hello":"world"}`))
	require.Equal(t, hex.EncodeToString(want[:]), artifact.Request.SHA256)
	require.Equal(t, []byte(`{"hello":"world"}`), artifact.Request.Bytes)
}

func TestMessageCaptureSpillsAndCleansUp(t *testing.T) {
	capture := NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 64, MemoryBudgetBytes: 4, SpoolDirectory: t.TempDir()})
	_, err := capture.RequestWriter().Write([]byte("0123456789"))
	require.NoError(t, err)
	artifact := capture.Artifact()
	require.Equal(t, BodyStateAvailable, artifact.Request.State)
	require.NotEmpty(t, artifact.Request.FilePath)
	_, err = os.Stat(artifact.Request.FilePath)
	require.NoError(t, err)
	require.NoError(t, artifact.Cleanup())
	_, err = os.Stat(artifact.Request.FilePath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMessageCaptureLimitDoesNotBlockResponse(t *testing.T) {
	capture := NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 4, MemoryBudgetBytes: 1024, SpoolDirectory: t.TempDir()})
	n, err := capture.ResponseWriter().Write([]byte("0123456789"))
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, BodyStateTooLarge, capture.Artifact().Response.State)
}

func TestMessageCaptureRequestTee(t *testing.T) {
	capture := NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 64, MemoryBudgetBytes: 1024, SpoolDirectory: t.TempDir()})
	tee := capture.WrapRequestBody(io.NopCloser(strings.NewReader("client-body")))
	got, err := io.ReadAll(tee)
	require.NoError(t, err)
	require.Equal(t, "client-body", string(got))
	require.Equal(t, int64(len("client-body")), capture.Artifact().Request.RawBytes)
}
