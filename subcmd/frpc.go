package subcmd

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/sevlyar/go-daemon"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/models/auth"
	"github.com/fatedier/frp/models/config"
	"github.com/fatedier/frp/models/consts"
	"github.com/fatedier/frp/utils/log"
	"github.com/fatedier/golib/crypto"
)

var (
	fserver string
	fuser   string
	fproto  string
	ftoken  string

	floglevel  string
	flogfile   string
	flogmaxday int64
	fproxyname string
	flocalip   string
	flocalport int

	fcustom string
	fsubdom string

	flocations string
	fhttpuser  string
	fhttppass  string

	fencryption  bool
	fcompression bool
	fdaemon      bool

	kcpDoneCh chan struct{}
)

const (
	CfgFileTypeIni = iota
	CfgFileTypeCmd
)

func RegisterFrpc(frpc *kingpin.CmdClause) {

	frpc.Flag("server_addr", "frp server's address").Short('s').Default("127.0.0.1:7000").StringVar(&fserver)
	frpc.Flag("user", "user").Short('u').Default("").StringVar(&fuser)
	frpc.Flag("protocol", "tcp or kcp or websocke").Short('p').Default("tcp").StringVar(&fproto)
	frpc.Flag("token", "token").Short('t').Default("").StringVar(&ftoken)

	frpc.Flag("log_level", "log level").Default("info").StringVar(&floglevel)
	frpc.Flag("log_file", "log file").Default("console").StringVar(&flogfile)
	frpc.Flag("log_max_days", "log file reversed days").Default("3").Int64Var(&flogmaxday)

	frpc.Flag("proxy_name", "proxy name").Short('n').Default("").StringVar(&fproxyname)
	frpc.Flag("local_ip", "local ip").Short('i').Default("127.0.0.1").StringVar(&flocalip)
	frpc.Flag("local_port", "local port").Short('l').Required().IntVar(&flocalport)

	frpc.Flag("custom_domain", "custom domain").Short('c').Default("").StringVar(&fcustom)
	frpc.Flag("sd", "sub domain").Default("").StringVar(&fsubdom)
	frpc.Flag("locations", "locations").Default("").StringVar(&flocations)

	frpc.Flag("http_user", "http auth user").StringVar(&fhttpuser)
	frpc.Flag("http_pass", "http auth pass").StringVar(&fhttppass)

	frpc.Flag("ue", "use encryption").BoolVar(&fencryption)
	frpc.Flag("uc", "use compression").BoolVar(&fcompression)

	frpc.Flag("daemon", "daemon mode").Short('d').BoolVar(&fdaemon)
}

func parseClientCommonCfg() (cfg config.ClientCommonConf, err error) {
	cfg, err = func() (cfg config.ClientCommonConf, err error) {
		cfg = config.GetDefaultClientConf()

		strs := strings.Split(fserver, ":")
		if len(strs) < 2 {
			err = fmt.Errorf("invalid server_addr")
			return
		}
		if strs[0] != "" {
			cfg.ServerAddr = strs[0]
		}
		cfg.ServerPort, err = strconv.Atoi(strs[1])
		if err != nil {
			err = fmt.Errorf("invalid server_addr")
			return
		}

		cfg.User = fuser
		cfg.Protocol = fproto
		cfg.LogLevel = floglevel
		cfg.LogFile = flogfile
		cfg.LogMaxDays = flogmaxday
		if flogfile == "console" {
			cfg.LogWay = "console"
		} else {
			cfg.LogWay = "file"
		}
		cfg.DisableLogColor = true

		// Only token authentication is supported in cmd mode
		cfg.AuthClientConfig = auth.GetDefaultAuthClientConf()
		cfg.Token = ftoken

		return
	}()
	if err != nil {
		return
	}

	err = cfg.Check()
	if err != nil {
		return
	}
	return
}

func startService(cfg config.ClientCommonConf, pxyCfgs map[string]config.ProxyConf, visitorCfgs map[string]config.VisitorConf, cfgFile string) (err error) {
	log.InitLog(cfg.LogWay, cfg.LogFile, cfg.LogLevel,
		cfg.LogMaxDays, cfg.DisableLogColor)

	if cfg.DnsServer != "" {
		s := cfg.DnsServer
		if !strings.Contains(s, ":") {
			s += ":53"
		}
		// Change default dns server for frpc
		net.DefaultResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return net.Dial("udp", s)
			},
		}
	}
	svr, errRet := client.NewService(cfg, pxyCfgs, visitorCfgs, cfgFile)
	if errRet != nil {
		err = errRet
		return
	}

	// Capture the exit signal if we use kcp.
	if cfg.Protocol == "kcp" {
		go func(svr *client.Service) {
			ch := make(chan os.Signal)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
			<-ch
			svr.Close()
			time.Sleep(250 * time.Millisecond)
			close(kcpDoneCh)
		}(svr)
	}

	err = svr.Run()
	if cfg.Protocol == "kcp" {
		<-kcpDoneCh
	}
	return
}

func DoFrpc() error {
	if fdaemon {
		fmt.Println("run atx-agent frpc in background")
		cntxt := func() (cntxt *daemon.Context) {
			cntxt = &daemon.Context{ // remove pid to prevent resource busy
				PidFilePerm: 0644,
				LogFilePerm: 0640,
				WorkDir:     "./",
				Umask:       022,
			}
			child, err := cntxt.Reborn()
			if err != nil {
				fmt.Printf("Unale to run:%v\n", err)
			}
			if child != nil {
				return nil // return nil indicate program run in parent
			}
			return cntxt
		}()
		if cntxt == nil {
			return nil
		}
		defer cntxt.Release()
		fmt.Print("- - - - - - - - - - - - - - -")
		fmt.Print("daemon started")
	}
	crypto.DefaultSalt = "frp"
	rand.Seed(time.Now().UnixNano())
	clientCfg, err := parseClientCommonCfg()
	if err != nil {
		fmt.Println(err)
		return err
	}
	cfg := &config.HttpProxyConf{}
	var prefix string
	if fuser != "" {
		prefix = fuser + "."
	}

	cfg.ProxyName = prefix + fproxyname
	cfg.ProxyType = consts.HttpProxy
	cfg.LocalIp = flocalip
	cfg.LocalPort = flocalport
	cfg.CustomDomains = strings.Split(fcustom, ",")
	cfg.SubDomain = fsubdom
	cfg.Locations = strings.Split(flocations, ",")
	cfg.HttpUser = fhttpuser
	cfg.HttpPwd = fhttppass
	cfg.UseEncryption = fencryption
	cfg.UseCompression = fcompression
	err = cfg.CheckForCli()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return err
	}
	proxyConfs := map[string]config.ProxyConf{
		cfg.ProxyName: cfg,
	}

	err = startService(clientCfg, proxyConfs, nil, "")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return err
	}
	return nil
}
