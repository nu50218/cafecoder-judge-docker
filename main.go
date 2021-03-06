package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cafecoder-dev/cafecoder-judge/src/types"
)

const (
	ContainerPort = "0.0.0.0:8887"
	MaxFileSize = 200000000 // 200MB
)

func main() {
	listen, err := net.Listen("tcp", ContainerPort) //from backend server
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		defer cnct.Close()

		var request types.RequestJSON

		json.NewDecoder(cnct).Decode(&request)

		go func() {
			os.Chmod("/", 0777)
			cmdResult := execCmd(request)

			cmdResult.StdoutSize = getFileSize("userStdout.txt")
			cmdResult.IsOLE = cmdResult.StdoutSize > MaxFileSize

			conn, err := net.Dial("tcp", getHostIP())
			if err != nil {
				conn.Write([]byte("tcp connect error"))
			}
			defer conn.Close()

			b, err := json.Marshal(cmdResult)
			if err != nil {
				conn.Write([]byte("marshal error"))
			}

			conn.Write(b)

			os.Remove("execCmd.sh")

		}()
	}
}

// 実行するコマンドをシェルスクリプトに書き込む
func makeSh(cmd string) error {
	f, err := os.Create("execCmd.sh")
	if err != nil {
		return err
	}

	f.WriteString("#!/bin/bash\n")
	f.WriteString("export PATH=$PATH:/usr/local/go/bin\n")
	f.WriteString("export PATH=\"$HOME/.cargo/bin:$PATH\"\n")
	f.WriteString(cmd + "\n")
	f.WriteString("echo $? > exit_code.txt")

	f.Close()

	os.Chmod("execCmd.sh", 0777)

	return nil
}

// 提出されたコードを実行する
func execCmd(request types.RequestJSON) types.CmdResultJSON {
	var (
		err error
		cmdResult types.CmdResultJSON
	)
	cmdResult.SessionID = request.SessionID

	if err := makeSh(request.Cmd); err != nil {
		log.Println(err)
		return cmdResult
	}

	cmd := exec.Command("sh", "-c", "/usr/bin/time -v ./execCmd.sh 2>&1 | grep -E 'Maximum' | awk '{ print $6 }' > mem_usage.txt")

	start := time.Now()
	timeout := time.After(2*time.Second + 200*time.Millisecond)

	if err := cmd.Start(); err != nil {
		cmdResult.ErrMessage = err.Error()
	}

	done := make(chan error)
	go func() { done <- cmd.Wait() }()

	select {
	case <-timeout:
		// timeout からシグナルが送られてきたらプロセスをキルする
		cmd.Process.Kill()
	case err := <-done:
		if err != nil {
			cmdResult.ErrMessage = err.Error()
		}
	}

	end := time.Now()

	cmdResult.Time = int((end.Sub(start)).Milliseconds())

	cmdResult.ErrMessage, err = getFileStrBase64("/userStderr.txt")
	if err != nil {
		log.Println(err)
	}

	memUsage, err := getFileNum("mem_usage.txt")
	if err != nil {
		log.Println(err)
	}
	cmdResult.MemUsage = memUsage

	exitCode, err := getFileNum("exit_code.txt")
	if err != nil {
		log.Println(err)
	}
	cmdResult.Result = exitCode == 0

	return cmdResult
}

// mem_usage.txt, ret_code.txt に数値が記述されているため、それを読み取ってくる
func getFileNum(name string) (int, error) {
	fp, err := os.Open(name)
	if err != nil {
		return 0, err
	}
	defer fp.Close()

	buf, err := ioutil.ReadAll(fp)

	tmp := strings.Replace(string(buf), "\n", "", -1)

	mem, err := strconv.Atoi(tmp)
	if err != nil {
		return 0, err
	}

	return mem, err
}

// return base64 encoded string
func getFileStrBase64(name string) (string, error) {
	stderrFp, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer stderrFp.Close()

	buf := make([]byte, 65536)

	buf, err = ioutil.ReadAll(stderrFp)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf) + "\n", nil
}

// ファイル(userStdout.txt)のサイズを読む
func getFileSize(name string) int64 {
	info, err := os.Stat(name)
	if err != nil {
		return 0
	}

	return info.Size()
}

// Windows 環境だとホストIPがかわることがあったからそれを読み取ってくるようにした
// しかし動かないどうして
func getHostIP() string {
	r, _ := exec.Command("sh", "-c", "ip route | awk 'NR==1 {print $3}'").Output()
	return strings.TrimRight(string(r), "\n") + ":3344"
}
