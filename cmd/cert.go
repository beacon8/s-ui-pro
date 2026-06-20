package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/service"
)

func certCmd(args []string) {
	fs := flag.NewFlagSet("cert", flag.ExitOnError)
	var (
		issueIP   bool
		issueSelf bool
		renew     bool
		status    bool
		remove    bool
		reload    bool
		force     bool
	)
	fs.BoolVar(&issueIP, "issue-ip", false, "申请 Let's Encrypt IP 证书")
	fs.BoolVar(&issueSelf, "issue-self", false, "生成自签证书")
	fs.BoolVar(&renew, "renew", false, "强制续签")
	fs.BoolVar(&status, "status", false, "显示证书状态")
	fs.BoolVar(&remove, "remove", false, "移除证书（恢复 HTTP）")
	fs.BoolVar(&reload, "reload", false, "通知运行中的面板热加载证书（acme.sh reloadcmd 使用）")
	fs.BoolVar(&force, "force", false, "强制操作（如未到期也重申）")
	fs.Parse(args)

	// reload 命令：找到运行中的 sui 进程并发 SIGUSR1
	if reload {
		certReload()
		return
	}

	// 其余命令需要初始化 DB
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println("初始化数据库失败：", err)
		os.Exit(1)
	}

	settingService := &service.SettingService{}
	panelService := &service.PanelService{}
	// reload 时 web server 不可用，传 nil
	certSvc := service.NewCertService(settingService, panelService, nil)

	switch {
	case issueIP:
		fmt.Println("正在申请 Let's Encrypt IP 证书，请稍候（约 30~60 秒）...")
		st, err := certSvc.IssueLeIPCert(force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "申请失败：%s\n", err)
			os.Exit(1)
		}
		fmt.Printf("申请成功！证书到期：%s（剩余 %d 天）\n", st.NotAfter.Format("2006-01-02"), st.DaysLeft)

	case issueSelf:
		fmt.Println("正在生成自签证书...")
		st, err := certSvc.IssueSelfSignedCert()
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成失败：%s\n", err)
			os.Exit(1)
		}
		fmt.Printf("自签证书生成成功！到期：%s\n", st.NotAfter.Format("2006-01-02"))

	case renew:
		fmt.Println("正在续签证书...")
		if err := certSvc.RenewIpCert(); err != nil {
			fmt.Fprintf(os.Stderr, "续签失败：%s\n", err)
			os.Exit(1)
		}
		fmt.Println("续签成功！")

	case status:
		certStatus()

	case remove:
		if err := certSvc.RemoveCert(); err != nil {
			fmt.Fprintf(os.Stderr, "移除失败：%s\n", err)
			os.Exit(1)
		}
		fmt.Println("证书已移除，面板将恢复 HTTP（进程即将重启）")

	default:
		fs.Usage()
	}
}

// certStatus 打印证书状态
func certStatus() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println("初始化数据库失败：", err)
		return
	}
	settingService := &service.SettingService{}
	panelService := &service.PanelService{}
	certSvc := service.NewCertService(settingService, panelService, nil)
	st, err := certSvc.GetCertStatus()
	if err != nil {
		fmt.Println("读取证书状态失败：", err)
		return
	}
	fmt.Printf("证书模式：%s\n", st.Mode)
	if !st.HasCert {
		fmt.Println("当前无有效证书")
		return
	}
	fmt.Printf("证书文件：%s\n", st.CertFile)
	fmt.Printf("私钥文件：%s\n", st.KeyFile)
	fmt.Printf("主题：%s\n", st.Subject)
	fmt.Printf("颁发者：%s\n", st.Issuer)
	if st.IP != "" {
		fmt.Printf("IP：%s\n", st.IP)
	}
	fmt.Printf("有效期：%s ~ %s\n", st.NotBefore.Format("2006-01-02"), st.NotAfter.Format("2006-01-02"))
	fmt.Printf("剩余天数：%s\n", strconv.Itoa(st.DaysLeft))
}

// certReload 通过 SIGUSR1 通知运行中的 sui 进程热加载证书
func certReload() {
	// 尝试通过 pgrep 找到 sui 进程
	out, err := exec.Command("pgrep", "-x", "sui").Output()
	if err != nil {
		// 找不到进程，静默退出（exit 0，避免 acme.sh 报错）
		return
	}
	pid := string(out)
	if pid == "" {
		return
	}
	// 发送 SIGUSR1
	exec.Command("kill", "-USR1", pid).Run()
}
