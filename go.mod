module github.com/openatx/atx-agent

require (
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/DeanThompson/syncmap v0.0.0-20170515023643-05cfe1984971
	github.com/alecthomas/kingpin v2.2.6+incompatible
	github.com/alecthomas/template v0.0.0-20160405071501-a0175ee3bccc // indirect
	github.com/alecthomas/units v0.0.0-20151022065526-2efee857e7cf // indirect
	github.com/codeskyblue/goreq v0.0.0-20180831024223-49450746aaef
	github.com/dsnet/compress v0.0.0-20171208185109-cc9eb1d7ad76 // indirect
	github.com/dustin/go-broadcast v0.0.0-20171205050544-f664265f5a66
	github.com/franela/goblin v0.0.0-20181003173013-ead4ad1d2727 // indirect
	github.com/franela/goreq v0.0.0-20171204163338-bcd34c9993f8
	github.com/getlantern/context v0.0.0-20181106182922-539649cc3118 // indirect
	github.com/getlantern/errors v0.0.0-20180829142810-e24b7f4ff7c7 // indirect
	github.com/getlantern/go-update v0.0.0-20170504001518-d7c3f1ac97f8
	github.com/getlantern/golog v0.0.0-20170508214112-cca714f7feb5 // indirect
	github.com/getlantern/hex v0.0.0-20160523043825-083fba3033ad // indirect
	github.com/getlantern/hidden v0.0.0-20160523043807-d52a649ab33a // indirect
	github.com/getlantern/ops v0.0.0-20170904182230-37353306c908 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/snappy v0.0.0-20180518054509-2e65f85255db // indirect
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/gorilla/mux v1.7.4
	github.com/gorilla/websocket v1.4.0
	github.com/kardianos/osext v0.0.0-20170510131534-ae77be60afb1 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/kr/binarydist v0.1.0 // indirect
	github.com/creack/pty v1.1.15
	github.com/levigross/grequests v0.0.0-20190130132859-37c80f76a0da
	github.com/mholt/archiver v2.0.1-0.20171012052341-26cf5bb32d07+incompatible
	github.com/mitchellh/ioprogress v0.0.0-20180201004757-6a23b12fa88e
	github.com/nwaples/rardecode v1.0.0 // indirect
	github.com/onsi/gomega v1.5.0 // indirect
	github.com/openatx/androidutils v1.0.0
	github.com/oxtoacart/bpool v0.0.0-20150712133111-4e1c5567d7c2 // indirect
	github.com/pierrec/lz4 v2.0.5+incompatible // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/procfs v0.0.2
	github.com/rs/cors v1.6.0
	github.com/sevlyar/go-daemon v0.1.4
	github.com/shogo82148/androidbinary v1.0.1
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749 // indirect
	github.com/shurcooL/vfsgen v0.0.0-20200824052919-0d455de96546 // indirect
	github.com/sirupsen/logrus v1.6.0
	github.com/stretchr/testify v1.4.0
	github.com/ulikunitz/xz v0.5.5 // indirect
	golang.org/x/net v0.0.0-20181114220301-adae6a3d119a // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)

replace (
	github.com/prometheus/procfs v0.0.2 => github.com/codeskyblue/procfs v0.0.0-20190614074311-71434f4ee4b7
	github.com/qiniu/log v0.0.0-20140728010919-a304a74568d6 => github.com/gobuild/log v1.0.0
	golang.org/x/net v0.0.0-20181114220301-adae6a3d119a => github.com/golang/net v0.0.0-20181114220301-adae6a3d119a
)

go 1.13
