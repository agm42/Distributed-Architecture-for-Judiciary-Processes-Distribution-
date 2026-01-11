/***************************************************************************
	CSC-27 / CE-288 - ITA - 2025, 2º sem. - Profs. Hirata and Juliana

	LabExam - Simulador de Tribunal de Justiça Descentralizado

	Students: 
	        Antonio Gilberto de Moura (A - AGM)
			Fernado Maurício Gomes (F - FMG)
			Rodrigo Freire dos Santos Alencar (R - RFA)

        Rel 1.0.0

        Copyright (c) 2025 by A/F/R.
        All Rights Reserved.


Revision History for tribunal.go:

   Release   Author   Date           Description
    1.0.0    A/F/R    19/NOV/2025    Initial stable release

***************************************************************************/

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"runtime"
	"os/exec"
)

// Identificação da release
const Release = "1.0.0"


// ---------- Estruturas de dados ----------

type Comarca struct {
	ID       int    `json:"id"`
	Nome     string `json:"nome"`
	Endereco string `json:"endereco"`
	Varas    int    `json:"varas"`
}

type ComarcaList struct {
	mu      sync.RWMutex
	Itens   []Comarca
	arqPath string
}


// ---------- Funções ----------

func NovaComarcaList(arqPath string) *ComarcaList {
	return &ComarcaList{
		Itens:   make([]Comarca, 0),
		arqPath: arqPath,
	}
}

func (cl *ComarcaList) Load() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	f, err := os.Open(cl.arqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var itens []Comarca
	if err := dec.Decode(&itens); err != nil {
		return err
	}
	cl.Itens = itens
	return nil
}

func (cl *ComarcaList) Save() error {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	tmp := cl.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(cl.Itens); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, cl.arqPath)
}

// gera próximo ID de comarca com base no maior ID existente
func (cl *ComarcaList) nextID() int {
	max := 0
	for _, c := range cl.Itens {
		if c.ID > max {
			max = c.ID
		}
	}
	return max + 1
}

// Agora Add gera e devolve a comarca com ID definido
func (cl *ComarcaList) Add(c Comarca) (Comarca, error) {
	cl.mu.Lock()
	if c.ID == 0 {
		c.ID = cl.nextID()
	}
	cl.Itens = append(cl.Itens, c)
	cl.mu.Unlock()

	if err := cl.Save(); err != nil {
		return Comarca{}, err
	}
	return c, nil
}

func (cl *ComarcaList) RemoveByName(name string) (*Comarca, error) {
	cl.mu.Lock()
	idx := -1
	var removed Comarca
	for i, c := range cl.Itens {
		if c.Nome == name {
			idx = i
			removed = c
			break
		}
	}
	if idx == -1 {
		cl.mu.Unlock()
		return nil, errors.New("comarca não encontrada")
	}
	cl.Itens = append(cl.Itens[:idx], cl.Itens[idx+1:]...)
	cl.mu.Unlock()

	if err := cl.Save(); err != nil {
		return nil, err
	}
	return &removed, nil
}

func (cl *ComarcaList) UpdateVaras(name string, varas int) (*Comarca, error) {
	cl.mu.Lock()
	idx := -1
	for i, c := range cl.Itens {
		if c.Nome == name {
			cl.Itens[i].Varas = varas
			idx = i
			break
		}
	}
	if idx == -1 {
		cl.mu.Unlock()
		return nil, errors.New("comarca não encontrada")
	}
	updated := cl.Itens[idx]
	cl.mu.Unlock()

	if err := cl.Save(); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (cl *ComarcaList) GetByName(name string) *Comarca {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	for _, c := range cl.Itens {
		if c.Nome == name {
			cp := c
			return &cp
		}
	}
	return nil
}

func (cl *ComarcaList) ListExcept(addr string) []Comarca {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	res := make([]Comarca, 0, len(cl.Itens))
	for _, c := range cl.Itens {
		if c.Endereco != addr {
			res = append(res, c)
		}
	}
	return res
}


// ---------- Protocolo UDP ----------

type Request struct {
	Type  string `json:"type"`
	Nome  string `json:"nome,omitempty"`
	Varas int    `json:"varas,omitempty"`
}

type Response struct {
	Success  bool      `json:"success"`
	Message  string    `json:"message"`
	Comarca  *Comarca  `json:"comarca,omitempty"`
	Comarcas []Comarca `json:"comarcas,omitempty"`
}

func handlePacket(conn net.PacketConn, addr net.Addr, data []byte, cl *ComarcaList) {
	log.Printf("[REQ] %s - pacote recebido de %s (%d bytes)",
		time.Now().Format(time.RFC3339), addr.String(), len(data))

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("[ERR] %s - erro ao decodificar requisição de %s: %v",
			time.Now().Format(time.RFC3339), addr.String(), err)
		sendResponse(conn, addr, Response{false, "erro ao decodificar requisição", nil, nil})
		return
	}

	log.Printf("[REQ] %s - de %s: type=%q nome=%q varas=%d",
		time.Now().Format(time.RFC3339), addr.String(), req.Type, req.Nome, req.Varas)

	switch req.Type {

	case "list":
		comarcas := cl.ListExcept(addr.String())
		sendResponse(conn, addr, Response{true, "ok", nil, comarcas})

	case "create":
		if req.Nome == "" || req.Varas <= 0 {
			sendResponse(conn, addr, Response{false, "campos 'nome' e 'varas' obrigatórios", nil, nil})
			return
		}
		existing := cl.GetByName(req.Nome)
		if existing != nil {
			sendResponse(conn, addr, Response{true, "comarca já existente", existing, nil})
			return
		}
		nova := Comarca{Nome: req.Nome, Endereco: addr.String(), Varas: req.Varas}
		nova, err := cl.Add(nova)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "comarca criada", &nova, nil})

	case "remove":
		if req.Nome == "" {
			sendResponse(conn, addr, Response{false, "campo 'nome' obrigatório", nil, nil})
			return
		}
		removed, err := cl.RemoveByName(req.Nome)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "comarca removida", removed, nil})

	case "update_varas":
		if req.Nome == "" {
			sendResponse(conn, addr, Response{false, "campo 'nome' obrigatório", nil, nil})
			return
		}
		updated, err := cl.UpdateVaras(req.Nome, req.Varas)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "número de varas atualizado", updated, nil})

	default:
		sendResponse(conn, addr, Response{false, "tipo de requisição desconhecido", nil, nil})
	}
}

func sendResponse(conn net.PacketConn, addr net.Addr, resp Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.WriteTo(b, addr)

	log.Printf("[RESP] %s - para %s: success=%v msg=%q comarcas=%d",
		time.Now().Format(time.RFC3339), addr.String(),
		resp.Success, resp.Message, len(resp.Comarcas))
}


// ---------- Utilitário: limpar tela ----------
func clearScreen() {
        switch runtime.GOOS {
        case "windows":
                // Para cmd / PowerShell
                cmd := exec.Command("cmd", "/c", "cls")
                cmd.Stdout = os.Stdout
                _ = cmd.Run()
        default:
                // Linux, macOS, MSYS2, etc.
                cmd := exec.Command("clear")
                cmd.Stdout = os.Stdout
                if err := cmd.Run(); err != nil {
                        // Se der erro, cai pro escape ANSI
                        fmt.Print("\033[2J\033[H")
                }
        }
}


// ---------- Menu via teclado ----------
func iniciarMenu(cl *ComarcaList, sair chan bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		fmt.Println("========== TRIBUNAL ==========")
		fmt.Println("1 (L) - Apresentar lista de comarcas")
		fmt.Println("2 (A) - Adicionar nova comarca")
		fmt.Println("3 (D) - Remover comarca")
		fmt.Println("4 (S) - Sair")
		fmt.Println("5 (R) - Refresh (limpar tela)")
		fmt.Print("Sua opção> ")

		linha, _ := reader.ReadString('\n')
		opc := strings.TrimSpace(linha)

		switch opc {

		case "5","r", "R":
			clearScreen()
			continue

		case "1","l","L":
			cl.mu.RLock()
			fmt.Println("\n--- COMARCAS ---")
			if len(cl.Itens) == 0 {
				fmt.Println("(lista vazia)")
			} else {
				for _, c := range cl.Itens {
					fmt.Printf("ID %d | %s | %s | %d varas\n",
						c.ID, c.Nome, c.Endereco, c.Varas)
				}
			}
			cl.mu.RUnlock()

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "2", "a", "A":
			fmt.Print("Nome da comarca: ")
			nome, _ := reader.ReadString('\n')
			nome = strings.TrimSpace(nome)

			fmt.Print("Endereço UDP da comarca: ")
			end, _ := reader.ReadString('\n')
			end = strings.TrimSpace(end)

			fmt.Print("Número de varas: ")
			vs, _ := reader.ReadString('\n')
			vs = strings.TrimSpace(vs)
			varas, err := strconv.Atoi(vs)
			if err != nil || varas < 0 {
				fmt.Println("Número de varas inválido.")

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			c := Comarca{Nome: nome, Endereco: end, Varas: varas}
			c, err = cl.Add(c)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Printf("Comarca adicionada: ID %d\n", c.ID)
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "3", "d", "D":
			fmt.Print("Nome da comarca a remover: ")
			nome, _ := reader.ReadString('\n')
			nome = strings.TrimSpace(nome)
			removed, err := cl.RemoveByName(nome)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Printf("Comarca removida: ID %d | %s\n", removed.ID, removed.Nome)
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "4", "s", "S":
			if err := cl.Save(); err != nil {
				fmt.Println("Erro ao salvar ao sair:", err)
			}
			sair <- true
			return

		default:
			fmt.Println("Opção inválida.")
			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()
		}
	}
}


// ---------- MAIN ----------

func main() {
	helpFlag := flag.Bool("h", false, "Mostrar help")
	addrFlag := flag.String("addr", "", "Endereço UDP do tribunal (default :9000)")
	logFlag := flag.String("log", "", "Arquivo de log (ou 'term' para log no terminal; default: tribunal.log)")
	flag.Parse()

	// Configuração de LOG
	if *logFlag == "" {
		logFile, err := os.OpenFile("tribunal.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Erro ao abrir arquivo de log padrão (tribunal.log):", err)
		} else {
			log.SetOutput(logFile)
		}
	} else if *logFlag == "term" {
		// mantém saída padrão (stderr)
	} else {
		logFile, err := os.OpenFile(*logFlag,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Erro ao abrir arquivo de log:", err)
		} else {
			log.SetOutput(logFile)
		}
	}

	if *helpFlag {
		fmt.Println("Programa utilizado para simular a descentralização do procedimento de inserir nova ação cível em uma das varas existentes nas diversas comarcas do Tribunal de Justiça do Estado de São Paulo.")
		fmt.Println("Release:", Release)
		fmt.Println()
		fmt.Println("Usage: tribunal [-h] [-info] [-addr <endereco UDP>] [-log <arquivo|term>]")
		return
	}

	udpAddr := ":9000"
	if strings.TrimSpace(*addrFlag) != "" {
		udpAddr = strings.TrimSpace(*addrFlag)
	}

	cl := NovaComarcaList("comarcas.json")
	if err := cl.Load(); err != nil {
		fmt.Println("Erro ao carregar comarcas do disco:", err)
	}

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Println("Servidor tribunal rodando em", udpAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()
		
	sair := make(chan bool)
	go iniciarMenu(cl, sair)

	conn, err := net.ListenPacket("udp", udpAddr)
	if err != nil {
		fmt.Println("Erro ao abrir UDP:", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 4096)

	for {
		select {
		case <-sair:
			return
		default:
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("Erro ao ler pacote UDP: %v", err)
				continue
			}

			data := make([]byte, n)
			copy(data, buf[:n])

			go handlePacket(conn, addr, data, cl)
		}
	}
}
