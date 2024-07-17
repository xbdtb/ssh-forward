package main

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

type Forward struct {
	Name             string `yaml:"name"`
	RemoteTargetHost string `yaml:"remoteTargetHost"`
	RemoteTargetPort int    `yaml:"remoteTargetPort"`
	LocalBindingPort int    `yaml:"localBindingPort"`
}

type Config struct {
	SshServer struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"sshServer"`
	Forwards []Forward `yaml:"forwards"`
}

type SshForward struct {
	sshClient *ssh.Client
}

func (self *SshForward) Start(config Config) {
	for {
		log.Printf("开始连接SSH服务器\n")
		sshConfig := &ssh.ClientConfig{
			User: config.SshServer.Username,
			Auth: []ssh.AuthMethod{
				ssh.Password(config.SshServer.Password),
			},
			Timeout:         5 * time.Second,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}

		sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", config.SshServer.Host, config.SshServer.Port), sshConfig)
		if err != nil {
			log.Printf("SSH服务器连接失败: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Printf("SSH服务器连接成功\n")

		signal := make(chan struct{})
		var once sync.Once

		go func() {
			for {
				session, err := sshClient.NewSession()
				if err != nil {
					log.Println("SSH服务器连接已断开:", err)
					once.Do(func() {
						_ = sshClient.Close()
						close(signal)
					})
					return
				}
				err = session.Run("echo")
				if err != nil {
					_ = session.Close()
					log.Println("SSH服务器连接已断开:", err)
					once.Do(func() {
						_ = sshClient.Close()
						close(signal)
					})
					return
				}
				_ = session.Close()
				time.Sleep(5 * time.Second)
			}
		}()

		self.sshClient = sshClient

		for _, forward := range config.Forwards {
			self.ForwardLoop(forward, signal)
		}
		_ = sshClient.Wait()
		time.Sleep(5 * time.Second)
	}
}

func (self *SshForward) ForwardLoop(forward Forward, ch <-chan struct{}) {
	localListener, err := net.Listen("tcp", fmt.Sprintf(":%d", forward.LocalBindingPort))
	if err != nil {
		log.Fatalf("%s 监听本地端口（%d）失败： %v\n", forward.Name, forward.LocalBindingPort, err)
		return
	}

	log.Printf("%s 监听成功：local: %d, remote: %d", forward.Name, forward.LocalBindingPort, forward.RemoteTargetPort)

	go func() {
		<-ch
		_ = localListener.Close()
	}()

	go func() {
		for {
			localConn, err := localListener.Accept()
			if err != nil {
				break
			}
			log.Println(forward.Name, "建立新连接")

			go func() {
				sshConn, err := self.sshClient.Dial("tcp", fmt.Sprintf("%s:%d", forward.RemoteTargetHost, forward.RemoteTargetPort))
				if err != nil {
					log.Printf("%s 无法建立SSH连接 %s:%d: %v\n", forward.Name, forward.RemoteTargetHost, forward.RemoteTargetPort, err)
					return
				}
				defer sshConn.Close()

				done := make(chan bool)

				// 从远程复制到本地
				go func() {
					_, err = io.Copy(localConn, sshConn)
					if err != nil {
						//log.Printf("%s Failed to copy from remote to local: %v", forward.Name, err)
					}
					done <- true
				}()

				// 从本地复制到远程
				go func() {
					_, _ = io.Copy(sshConn, localConn)
					done <- true
				}()

				<-done
				log.Println(forward.Name, "连接已结束")
			}()
		}
	}()
}

func createForward(sshClient *ssh.Client, forward Forward) {
	// 监听本地端口
	localListener, err := net.Listen("tcp", fmt.Sprintf(":%d", forward.LocalBindingPort))
	if err != nil {
		log.Printf("%s Failed to listen on local port: %v", forward.Name, err)
	}

	log.Printf("%s 监听成功", forward.Name)

	go func() {
		defer localListener.Close()
		for {
			// 接受本地连接并启动端口转发
			localConn, err := localListener.Accept()
			if err != nil {
				log.Printf("%s Failed to accept local connection: %v", forward.Name, err)
				continue
			}
			log.Println(forward.Name, "new connection")

			go func() {
				defer localConn.Close()

				// 建立SSH通道
				sshConn, err := sshClient.Dial("tcp", fmt.Sprintf("%s:%d", forward.RemoteTargetHost, forward.RemoteTargetPort))
				if err != nil {
					log.Printf("%s Failed to establish SSH connection to %s:%d: %v", forward.Name, forward.RemoteTargetHost, forward.RemoteTargetPort, err)
					return
				}
				defer sshConn.Close()

				done := make(chan bool)

				// 从远程复制到本地
				go func() {
					_, err = io.Copy(localConn, sshConn)
					if err != nil {
						//log.Printf("%s Failed to copy from remote to local: %v", forward.Name, err)
					}
					done <- true
				}()

				// 从本地复制到远程
				go func() {
					_, err = io.Copy(sshConn, localConn)
					if err != nil {
						//log.Printf("%s Failed to copy from local to remote: %v", forward.Name, err)
					}
					done <- true
				}()

				<-done
				log.Println(forward.Name, "end connection")
			}()
		}
	}()
}

func createForwards(config Config) {
	// 创建SSH客户端配置
	sshConfig := &ssh.ClientConfig{
		User: config.SshServer.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(config.SshServer.Password),
		},
		Timeout:         5 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// 连接SSH服务器
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", config.SshServer.Host, config.SshServer.Port), sshConfig)
	if err != nil {
		log.Fatalf("Failed to connect to SSH server: %v", err)
	}
	defer sshClient.Close()

	for _, forward := range config.Forwards {
		createForward(sshClient, forward)
	}

	sshClient.Wait()
}

func main() {
	log.Println(os.Args)
	configFile, err := os.ReadFile(".sshforwardrc")
	if err != nil {
		log.Fatalf("无法读取配置文件: %v", err)
	}
	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		log.Fatalf("无法解析配置文件: %v", err)
	}
	//createForwards(config)
	//select {}
	sshForward := SshForward{}
	sshForward.Start(config)
}
