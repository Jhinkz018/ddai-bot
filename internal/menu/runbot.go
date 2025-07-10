package menu

import (
	"bufio"
	"ddai-bot/internal/ddai"
	"ddai-bot/internal/proxy"
	"ddai-bot/internal/utils"
	"fmt"
	"os"
	"strings"
	"sync"
)

type account struct {
	email    string
	password string
}

func (m *MenuHandler) loadRunAccounts() ([]account, error) {
	file, err := os.Open("runaccounts.txt")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var accounts []account
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		accounts = append(accounts, account{email: parts[0], password: parts[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (m *MenuHandler) runConcurrent(accounts []account) {
	proxy.LoadProxies()
	total := len(accounts)
	var wg sync.WaitGroup
	for idx, acc := range accounts {
		wg.Add(1)
		go func(i int, a account) {
			defer wg.Done()
			prx, _ := proxy.GetRandomProxy(i+1, total)
			bot := ddai.NewDdaiRunBot(a.email, a.password, prx, i+1, total)
			if err := bot.SingleProses(); err != nil {
				utils.LogMessage(i+1, total, fmt.Sprintf("Run failed: %v", err), "warning")
			}
		}(idx, acc)
	}
	wg.Wait()
}

func (m *MenuHandler) runQueue(accounts []account) {
	proxy.LoadProxies()
	total := len(accounts)
	for idx, acc := range accounts {
		prx, _ := proxy.GetRandomProxy(idx+1, total)
		bot := ddai.NewDdaiRunBot(acc.email, acc.password, prx, idx+1, total)
		if err := bot.SingleProses(); err != nil {
			utils.LogMessage(idx+1, total, fmt.Sprintf("Run failed: %v", err), "warning")
		}
	}
}

func (m *MenuHandler) RunAutoBot() {
	accounts, err := m.loadRunAccounts()
	if err != nil {
		utils.LogMessage(0, 0, "Failed to load runaccounts.txt: "+err.Error(), "error")
		m.waitForEnter()
		return
	}
	if len(accounts) == 0 {
		utils.LogMessage(0, 0, "No accounts found in runaccounts.txt", "error")
		m.waitForEnter()
		return
	}

	choice := m.showBotModeMenu()
	switch choice {
	case "1":
		m.runConcurrent(accounts)
	case "2":
		m.runQueue(accounts)
	default:
		return
	}

	utils.LogMessage(0, 0, "Finished running accounts", "success")
	m.waitForEnter()
}
