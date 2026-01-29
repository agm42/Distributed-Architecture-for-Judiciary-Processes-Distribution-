/***************************************************************************
	Distributed Architecture for Judiciary Processes Distribution
	===== Court Agent ====

	Authors: 
	        Antonio Gilberto de Moura (A - AGM)
		Fernado Maurício Gomes (F - FMG)

        Rel 1.1.0


Revision History for court.go:

   Release   Author   Date           Description
    1.0.0    A/F      19/Nov/2025    Initial stable release
    1.1.0    A        28/Jan/2026    Translation to English

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

// Release identification
const Release = "1.1.0" // Translation to English


// ---------- Data Structures ----------

type District struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Trials   int    `json:"trials"`
}

type DistrictList struct {
	mu      sync.RWMutex
	Items   []District
	arqPath string
}


// ---------- Functions ----------

func NewDistrictList(arqPath string) *DistrictList {
	return &DistrictList{
		Items:   make([]District, 0),
		arqPath: arqPath,
	}
}

func (dl *DistrictList) Load() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	f, err := os.Open(dl.arqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var items []District
	if err := dec.Decode(&items); err != nil {
		return err
	}
	dl.Items = items
	return nil
}

func (dl *DistrictList) Save() error {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	tmp := dl.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(dl.Items); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, dl.arqPath)
}

// Generate the next district ID based in the greater ID already existent
func (dl *DistrictList) nextID() int {
	max := 0
	for _, d := range dl.Items {
		if d.ID > max {
			max = d.ID
		}
	}
	return max + 1
}

// Add generates and returns a district with ID defined
func (dl *DistrictList) Add(d District) (District, error) {
	dl.mu.Lock()
	if d.ID == 0 {
		d.ID = dl.nextID()
	}
	dl.Items = append(dl.Items, d)
	dl.mu.Unlock()

	if err := dl.Save(); err != nil {
		return District{}, err
	}
	return d, nil
}

func (dl *DistrictList) RemoveByName(name string) (*District, error) {
	dl.mu.Lock()
	idx := -1
	var removed District 
	for i, d := range dl.Items {
		if d.Name == name {
			idx = i
			removed = d
			break
		}
	}
	if idx == -1 {
		dl.mu.Unlock()
		return nil, errors.New("district not found")
	}
	dl.Items = append(dl.Items[:idx], dl.Items[idx+1:]...)
	dl.mu.Unlock()

	if err := dl.Save(); err != nil {
		return nil, err
	}
	return &removed, nil
}

func (dl *DistrictList) UpdateTrials(name string, trials int) (*District, error) {
	dl.mu.Lock()
	idx := -1
	for i, d := range dl.Items {
		if d.Name == name {
			dl.Items[i].Trials = trials 
			idx = i
			break
		}
	}
	if idx == -1 {
		dl.mu.Unlock()
		return nil, errors.New("district not found")
	}
	updated := dl.Items[idx]
	dl.mu.Unlock()

	if err := dl.Save(); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (dl *DistrictList) GetByName(name string) *District {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	for _, d := range dl.Items {
		if d.Name == name {
			dp := d
			return &dp
		}
	}
	return nil
}

func (dl *DistrictList) ListExcept(addr string) []District {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	res := make([]District, 0, len(dl.Items))
	for _, d := range dl.Items {
		if d.Address != addr {
			res = append(res, d)
		}
	}
	return res
}


// ---------- UDP Protocol ----------

type Request struct {
	Type   string `json:"type"`
	Name   string `json:"name,omitempty"`
	Trials int    `json:"trials,omitempty"`
}

type Response struct {
	Success  bool        `json:"success"`
	Message  string      `json:"message"`
	District *District   `json:"district,omitempty"`
	Districts []District `json:"districts,omitempty"`
}

func handlePacket(conn net.PacketConn, addr net.Addr, data []byte, dl *DistrictList) {
	log.Printf("[REQ] %s - package received from %s (%d bytes)",
		time.Now().Format(time.RFC3339), addr.String(), len(data))

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("[ERR] %s - error for requisition decodification from %s: %v",
			time.Now().Format(time.RFC3339), addr.String(), err)
		sendResponse(conn, addr, Response{false, "error for requisition decodification", nil, nil})
		return
	}

	log.Printf("[REQ] %s - from %s: type=%q name=%q trials=%d",
		time.Now().Format(time.RFC3339), addr.String(), req.Type, req.Name, req.Trials)

	switch req.Type {

	case "list":
		districts := dl.ListExcept(addr.String())
		sendResponse(conn, addr, Response{true, "ok", nil, districts})

	case "create":
		if req.Name == "" || req.Trials <= 0 {
			sendResponse(conn, addr, Response{false, "fields 'name' and 'trials' are required", nil, nil})
			return
		}
		existing := dl.GetByName(req.Name)
		if existing != nil {
			sendResponse(conn, addr, Response{true, "district already existent", existing, nil})
			return
		}
		new_d := District{Name: req.Name, Address : addr.String(), Trials: req.Trials}
		new_d, err := dl.Add(new_d)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "district created", &new_d, nil})

	case "remove":
		if req.Name == "" {
			sendResponse(conn, addr, Response{false, "field 'name' is required", nil, nil})
			return
		}
		removed, err := dl.RemoveByName(req.Name)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "district removed", removed, nil})

	case "update_trials":
		if req.Name == "" {
			sendResponse(conn, addr, Response{false, "field 'name' is required", nil, nil})
			return
		}
		updated, err := dl.UpdateTrials(req.Name, req.Trials)
		if err != nil {
			sendResponse(conn, addr, Response{false, err.Error(), nil, nil})
			return
		}
		sendResponse(conn, addr, Response{true, "trials number updated", updated, nil})

	default:
		sendResponse(conn, addr, Response{false, "unknown type of request", nil, nil})
	}
}

func sendResponse(conn net.PacketConn, addr net.Addr, resp Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.WriteTo(b, addr)

	log.Printf("[RESP] %s - to %s: success=%v msg=%q districts=%d",
		time.Now().Format(time.RFC3339), addr.String(),
		resp.Success, resp.Message, len(resp.Districts))
}


// ---------- Clear the screen ----------
func clearScreen() {
        switch runtime.GOOS {
        case "windows":
                // For cmd / PowerShell
                cmd := exec.Command("cmd", "/c", "cls")
                cmd.Stdout = os.Stdout
                _ = cmd.Run()
        default:
                // Linux, macOS, MSYS2, etc.
                cmd := exec.Command("clear")
                cmd.Stdout = os.Stdout
                if err := cmd.Run(); err != nil {
                        // If error, goes to ANSI scape
                        fmt.Print("\033[2J\033[H")
                }
        }
}


// ---------- Menu throught keyboard ----------
func startMenu(dl *DistrictList, quit chan bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println() 
		fmt.Println("========== COURT OF JUSTICE ==========")
		fmt.Println("1 (L) - Show districts list")
		fmt.Println("2 (A) - Add a new district")
		fmt.Println("3 (D) - Delete a district")
		fmt.Println("4 (Q) - Quit")
		fmt.Println("5 (R) - Refresh (clear the screen)")
		fmt.Print("Your option> ")

		line, _ := reader.ReadString('\n')
		opt := strings.TrimSpace(line)

		switch opt {

		case "5","r", "R":
			clearScreen()
			continue

		case "1","l","L":
			dl.mu.RLock()
			fmt.Println("\n--- DISTRICTS ---")
			if len(dl.Items) == 0 {
				fmt.Println("(empty list)")
			} else {
				for _, d := range dl.Items {
					fmt.Printf("ID %d | %s | %s | %d trials\n",
						d.ID, d.Name, d.Address, d.Trials)
				}
			}
			dl.mu.RUnlock()

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "2", "a", "A":
			fmt.Print("District's name: ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)

			fmt.Print("District's UDP address: ")
			add, _ := reader.ReadString('\n')
			add = strings.TrimSpace(add)

			fmt.Print("Number of trials: ")
			ts, _ := reader.ReadString('\n')
			ts = strings.TrimSpace(ts)
			trials, err := strconv.Atoi(ts)
			if err != nil || trials < 0 {
				fmt.Println("Invalid number of trials.")

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			d := District{Name: name, Address: add, Trials: trials}
			d, err = dl.Add(d)
			if err != nil {
				fmt.Println("Error:", err)
			} else {
				fmt.Printf("District added: ID %d\n", d.ID)
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "3", "d", "D":
			fmt.Print("Name of the district to be removed: ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)
			removed, err := dl.RemoveByName(name)
			if err != nil {
				fmt.Println("Error:", err)
			} else {
				fmt.Printf("District removed: ID %d | %s\n", removed.ID, removed.Name)
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "4", "q", "Q":
			if err := dl.Save(); err != nil {
				fmt.Println("Saving error:", err)
			}
			quit <- true
			return

		default:
			fmt.Println("Invalid Option.")
			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()
		}
	}
}


// ---------- MAIN ----------

func main() {
	helpFlag := flag.Bool("h", false, "Show help")
	infoFlag := flag.Bool("info", false, "Show information about option flags")
	addrFlag := flag.String("addr", "", "Court's UDP address (default :9000)")
	logFlag := flag.String("log", "", "Log file (or 'term' to log to terminal; default: court.log)")
	flag.Parse()

	if *helpFlag {
		fmt.Println("Program used to simulate the decentralization of the procedure for filing")
		fmt.Println("a new civil lawsuit in one of the existing trials in the various districts")
	        fmt.Println("of the Court of Justice of the State of São Paulo.")
		fmt.Println("\n Release:", Release)
		fmt.Println()
		fmt.Println("Usage: court [-h] [-info] [-addr <UDP address>] [-log <file|term>]")
		return
	}

	// Uses -info as the default behavior for -h
	if *infoFlag {
		flag.Usage()
		os.Exit(0)
	}

	// LOG configuration
	if *logFlag == "" {
		logFile, err := os.OpenFile("court.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error after trying to open the default log file (court.log):", err)
		} else {
			log.SetOutput(logFile)
		}
	} else if *logFlag == "term" {
		// default out (stderr)
	} else {
		logFile, err := os.OpenFile(*logFlag,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error after trying to open the log file:", err)
		} else {
			log.SetOutput(logFile)
		}
	}

	udpAddr := ":9000"
	if strings.TrimSpace(*addrFlag) != "" {
		udpAddr = strings.TrimSpace(*addrFlag)
	}

	dl := NewDistrictList("districts.json")
	if err := dl.Load(); err != nil {
		fmt.Println("Error after trying to load districts list from the disc:", err)
	}

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Println("Court Server running in", udpAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()
		
	quit := make(chan bool)
	go startMenu(dl, quit)

	conn, err := net.ListenPacket("udp", udpAddr)
	if err != nil {
		fmt.Println("Error after trying to open UDP:", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 4096)

	for {
		select {
		case <-quit:
			return
		default:
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("Error after trying to read UDP package: %v", err)
				continue
			}

			data := make([]byte, n)
			copy(data, buf[:n])

			go handlePacket(conn, addr, data, dl)
		}
	}
}
