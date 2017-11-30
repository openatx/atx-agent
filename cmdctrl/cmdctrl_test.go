package cmdctrl

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	debug = true
}

func TestProcessKeeperStartStop(t *testing.T) {
	cmdInfo := CommandInfo{
		Args:            []string{"sleep", "1"},
		MaxRetries:      5,
		RecoverDuration: 2 * time.Second,
		NextLaunchWait:  1 * time.Second,
	}
	pkeeper := processKeeper{
		cmdInfo: cmdInfo,
	}
	assert.Nil(t, pkeeper.start())
	time.Sleep(500 * time.Millisecond) // 0.5s
	assert.True(t, pkeeper.keeping)
	assert.True(t, pkeeper.running)

	time.Sleep(1 * time.Second) // 1.5s
	assert.True(t, pkeeper.keeping)
	assert.False(t, pkeeper.running)

	time.Sleep(1500 * time.Millisecond) // 2.5s
	assert.True(t, pkeeper.keeping)
	assert.True(t, pkeeper.running)

	pkeeper.stop(true)
	assert.False(t, pkeeper.keeping)
	assert.False(t, pkeeper.running)

	// stop again
	assert.NotNil(t, pkeeper.stop(true))
	assert.False(t, pkeeper.keeping)
	assert.False(t, pkeeper.running)

	assert.Nil(t, pkeeper.start())
	assert.Nil(t, pkeeper.stop(false))
}

func TestCommandCtrl(t *testing.T) {
	assert := assert.New(t)
	service := New()
	addErr := service.Add("mysleep", CommandInfo{
		Args: []string{"sleep", "10"},
	})
	assert.Nil(addErr)
	assert.Nil(service.Start("mysleep"))
	assert.NotNil(service.Start("mysleep"))

	// duplicate add
	addErr = service.Add("mysleep", CommandInfo{
		Args: []string{"sleep", "20"},
	})
	assert.NotNil(addErr)

	assert.Nil(service.UpdateArgs("mysleep", "sleep", "30"))
	assert.Equal(service.cmds["mysleep"].cmdInfo.Args, []string{"sleep", "30"})
	assert.Nil(service.Stop("mysleep"))
}
