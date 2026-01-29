/***************************************************************************
        Distributed Architecture for Judiciary Processes Distribution
        ===== District Agent ====

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
	"flag"
	"fmt"
	"log"
	"math/rand"
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


// ---------- Structs shared with the Court ----------

type District struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Trials   int    `json:"trials"`
}

type Request struct {
	Type        string `json:"type"`             // "list", "create", "remove", "update_trials"
	Name        string `json:"name,omitempty"`   // used in create/remove/update_trials
	Trials      int    `json:"trials,omitempty"` // create / update_trials
	TrialsDelta int    `json:"trials_delta,omitempty"`
}

type Response struct {
	Success  bool        `json:"success"`
	Message  string      `json:"message"`
	District  *District  `json:"district,omitempty"`
	Districts []District `json:"districts,omitempty"`
}


// ---------- Structs to communication DISTRICT <-> TRIAL ----------

type DistrictInfoRequest struct {
	Type    string `json:"type"`     // "trial_info"
	TrialID int    `json:"trial_id"` // trial id (1, 2, 3, etc.)
}

type DistrictInfoResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`
}


// ---------- Lawsuits verification / distribution (DISTRICT -> TRIAL) ----------

// Description for the lawsuit that will be verified/created
type ActionQuery struct {
	Plaintiff string `json:"plaintiff"`
	Defendant string `json:"defendant"`
	CauseID   int    `json:"cause_id"`
	Claims    []int  `json:"claims"`
}

// Request from a district to a trial to look for a lawsuit inside its lists
// "Stage" correlated with the rules: "res_judicata", "lis_pendens", "repeated_request", "joinder", "connection"
type TrialActionQueryRequest struct {
	Type  string        `json:"type"`   // "lawsuit_query"
	Stage string        `json:"stage"`  // see above
	Lawsuit ActionQuery `json:"lawsuit"`
}

// Response from a trial about a lawsuit
// Match can be:
//   - "" or "none"
//   - "res_judicata"
//   - "lis_pendes"
//   - "repeated_request"
//   - "contained_joinder"
//   - "contingent_joinder"
//   - "connection"
type TrialActionQueryResponse struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`
	Match   string `json:"match"`
	Message string `json:"message"`

	LawsuitID string `json:"lawsuit_id,omitempty"`

	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`

	ExistentClaims    []int    `json:"existent_claims,omitempty"`
	ConnectedLawsuits []string `json:"connected_lawsuits,omitempty"`
}

// Request to create the lawsuit in the trial
// Reason: "free", "repeated_request", "connection"
type TrialCreateActionRequest struct {
	Type        string      `json:"type"` // "lawsuit_create"
	Reason      string      `json:"reason"`
	Lawsuit     ActionQuery `json:"lawsuit"`
	Related     string      `json:"related,omitempty"` // ID for the related lawsuit (repeated request, connection, etc.)
}

type TrialCreateActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	LawsuitID    string `json:"lawsuit_id,omitempty"`
	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`
}

// Request to update the lawsuit's requests (containment: joinder)
type TrialMergeClaimsRequest struct {
	Type       string `json:"type"` // "lawsuit_merge_claims"
	LawsuitID  string `json:"lawsuit_id"`
	NewClaims  []int  `json:"new_claims"`
}

type TrialMergeClaimsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}


// ---------- NEW: Lawsuits search (DISTRICT -> TRIAL) ----------

// Generic Search request (field + value) sent by district to each trial.
// Type = "search_lawsuit".
type TrialSearchLawsuitsRequest struct {
	Type  string `json:"type"`  // "search_lawsuit"
	Field string `json:"field"` // "id", "plaintiff", "defendant", "cause", "claim"
	Value string `json:"value"`
}

// Individual result returned by the trial for each lawsuit found
type TrialSearchLawsuitsResult struct {
	List        string `json:"list"`         // "Active", "Extinguished with merit", "Extinguished without merit"
	ID          string `json:"id"`           // Lawsuit's ID 
	Plaintiff   string `json:"plaintiff"`    // Plaintiff name
	Defendant   string `json:"defendant"`    // Defendant's name
	CauseAction int    `json:"cause_action"` // Cause of acton ID
	Claims      []int  `json:"claims"`       // Claims' list
}

// Trial's response with list of the lawsuits that meet the criteria
type TrialSearchLawsuitsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`

	Results []TrialSearchLawsuitsResult`json:"results,omitempty"`
}

// Workload verification for a trial (number of active lawsuits)
type TrialWorkloadRequest struct {
	Type string `json:"type"` // "workload_info"
}

type TrialWorkloadResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	DistrictID     int    `json:"district_id,omitempty"`
	DistrictName   string `json:"district_name,omitempty"`
	TrialID        int    `json:"trial_id,omitempty"`
	ActiveWorkload int    `json:"active_workload"`
}


// ---------- Local list of districts (mirror of Court) ----------

type DistrictList struct {
	mu      sync.RWMutex
	Items   []District
	arqPath string
}

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

func (dl *DistrictList) SetAll(list []District) error {
	dl.mu.Lock()
	dl.Items = list
	dl.mu.Unlock()
	return dl.Save()
}

func (dl *DistrictList) GetAll() []District {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	res := make([]District, len(dl.Items))
	copy(res, dl.Items)
	return res
}


// ---------- Local list of district's trials ----------

type Trial struct {
	ID       int    `json:"id"`
	Address  string `json:"address"`
}

type TrialList struct {
	mu      sync.RWMutex
	Items   []Trial
	arqPath string
}

func NewTrialList(arqPath string) *TrialList {
	return &TrialList{
		Items:   make([]Trial, 0),
		arqPath: arqPath,
	}
}

func (tl *TrialList) Load() error {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	f, err := os.Open(tl.arqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var items []Trial
	if err := dec.Decode(&items); err != nil {
		return err
	}
	tl.Items = items
	return nil
}

func (tl *TrialList) Save() error {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	tmp := tl.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tl.Items); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, tl.arqPath)
}

// next simple ID
func (tl *TrialList) nextID() int {
	max := 0
	for _, t := range tl.Items {
		if t.ID > max {
			max = t.ID
		}
	}
	return max + 1
}

func (tl *TrialList) Add(address string) (Trial, error) {
	tl.mu.Lock()
	t := Trial{
		ID:      tl.nextID(),
		Address: address,
	}
	tl.Items = append(tl.Items, t)
	tl.mu.Unlock()

	if err := tl.Save(); err != nil {
		return Trial{}, err
	}
	return t, nil
}

func (tl *TrialList) RemoveByID(id int) (Trial, error) {
	tl.mu.Lock()
	idx := -1
	var removed Trial
	for i, t := range tl.Items {
		if t.ID == id {
			idx = i
			removed = t
			break
		}
	}
	if idx == -1 {
		tl.mu.Unlock()
		return Trial{}, fmt.Errorf("trial with ID %d not found", id)
	}
	tl.Items = append(tl.Items[:idx], tl.Items[idx+1:]...)
	tl.mu.Unlock()

	if err := tl.Save(); err != nil {
		return Trial{}, err
	}
	return removed, nil
}

func (tl *TrialList) GetAll() []Trial {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	res := make([]Trial, len(tl.Items))
	copy(res, tl.Items)
	return res
}

func (tl *TrialList) Count() int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return len(tl.Items)
}

// New: search trial by ID (used by the response to trial_info)
func (tl *TrialList) FindByID(id int) (Trial, bool) {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	for _, t := range tl.Items {
		if t.ID == id {
			return t, true
		}
	}
	return Trial{}, false
}


// ---------- Persistence for district's NAME and ADDRESS ----------

const nameDistrictFile = "district_name.txt"
const addrDistrictFile = "district_addr.txt"

func loadDistrictName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error after trying to read the names' file for district (%s): %v", path, err)
		}
		return ""
	}
	name := strings.TrimSpace(string(b))
	return name
}

func saveNameDistrict(path, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if err := os.WriteFile(path, []byte(name+"\n"), 0644); err != nil {
		log.Printf("Error after trying to save the district's name in %s: %v", path, err)
	}
}

func loadDistrictAddress(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error after trying to read the address' file for district (%s): %v", path, err)
		}
		return ""
	}
	addr := strings.TrimSpace(string(b))
	return addr
}

func saveAddressDistrict(path, addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	if err := os.WriteFile(path, []byte(addr+"\n"), 0644); err != nil {
		log.Printf("Error after trying to save the district's address in %s: %v", path, err)
	}
}


// ---------- Communication with the Court ----------

func sendToCourt(courtAddr string, req Request) (Response, error) {
	var resp Response

	addr, err := net.ResolveUDPAddr("udp", courtAddr)
	if err != nil {
		return resp, fmt.Errorf("error after trying to resolve the Court's address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return resp, fmt.Errorf("error after trying to connect to the Court: %v", err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("error while coding JSON: %v", err)
	}

	log.Printf("[DISTRICT->COURT] %s - sending req type=%q name=%q trials=%d to %s",
		time.Now().Format(time.RFC3339),
		req.Type, req.Name, req.Trials,
		courtAddr,
	)

	if _, err := conn.Write(data); err != nil {
		return resp, fmt.Errorf("error while sending UDP: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return resp, fmt.Errorf("error while receiving response from the Court: %v", err)
	}

	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return resp, fmt.Errorf("error while decoding response JSON: %v", err)
	}

	log.Printf("[COURT->DISTRICT] %s - response success=%v msg=%q districts=%d",
		time.Now().Format(time.RFC3339),
		resp.Success, resp.Message, len(resp.Districts),
	)

	return resp, nil
}

func updateDistrictsOfCourt(courtAddr string, dl *DistrictList) error {
	req := Request{Type: "list"}
	resp, err := sendToCourt(courtAddr, req)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("court responded with error: %s", resp.Message)
	}
	if err := dl.SetAll(resp.Districts); err != nil {
		return fmt.Errorf("error while saving local districts' list: %v", err)
	}
	return nil
}

func sendUpdateTrials(courtAddr, nameDistrict string, totalTrials int) error {
	req := Request{
		Type:  "update_trials",
		Name:  nameDistrict,
		Trials: totalTrials,
	}
	_, err := sendToCourt(courtAddr, req)
	return err
}


// ---------- Specific handler for "trial_info" ----------

func handleTrialInfo(conn *net.UDPConn, remote *net.UDPAddr, data []byte, nameDistrict string, dl *DistrictList, tl *TrialList) {
	var req DistrictInfoRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar DistrictInfoRequest: %v", err)
		log.Printf("Error while decoding DistrictInfoRequest: %v", err)
		return
	}

	log.Printf("[TRIAL->DISTRICT] %s - trial_info received from %s (TrialID=%d)",
		time.Now().Format(time.RFC3339),
		remote.String(), req.TrialID,
	)

	// Search district's ID from the local mirror (if existent)
	districtID := 0
	districts := dl.GetAll()
	for _, d := range districts {
		if d.Name == nameDistrict {
			districtID = d.ID
			break
		}
	}

	// Search a trial by ID
	t, ok := tl.FindByID(req.TrialID)
	if !ok {
		resp := DistrictInfoResponse{
			Success: false,
			Message: fmt.Sprintf("Trial with ID %d not found in this district.", req.TrialID),
		}
		b, _ := json.Marshal(resp)
		_, _ = conn.WriteToUDP(b, remote)
		log.Printf("[DISTRICT->TRIAL] trial_info fault for %s (TrialID=%d): not found",
			remote.String(), req.TrialID)
		return
	}

	// Assemble the response 
	resp := DistrictInfoResponse{
		Success:     true,
		Message:     "Information from the trial sucessfully obtained.",
		DistrictID:   districtID,
		DistrictName: nameDistrict,
		TrialID:      t.ID,
		TrialAddr:    t.Address,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while coding response trial_info: %v", err)
		return
	}

	if _, err := conn.WriteToUDP(b, remote); err != nil {
		log.Printf("Error while sending response trial_info to %s: %v", remote.String(), err)
		return
	}

	log.Printf("[DISTRICT->TRIAL] trial_info OK for %s (TrialID=%d, Addr=%s, DistrictID=%d, Name=%s)",
		remote.String(), t.ID, t.Address, districtID, nameDistrict)
}


// ---------- Handler for "lawsuit_query" from the OTHER DISTRICT ----------

// This handler enables that ONE district act as "aggregator" of its trials
// for another district. The other district send a TrialActionQueryRequest (lawsuit_query)
// straight to the district's address, and here it is forwarded to ALL the local trials
// with searchTrialsLocalStage and it is returned a TrialActionQueryResponse
func handleActionQueryDistrict(
	conn *net.UDPConn,
	remote *net.UDPAddr,
	data []byte,
	nameDistrict string,
	dl *DistrictList,
	tl *TrialList,
) {
	var req TrialActionQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error while decoding TrialActionQueryRequest (from %s): %v", remote.String(), err)
		return
	}

	log.Printf("[DISTRICT<-TRIAL] %s - lawsuit_query stage=%s received from  %s",
		time.Now().Format(time.RFC3339), req.Stage, remote.String())

	// Convert ActionQuery -> NewLawsuit to reuse searchTrialsLocalStage
	new_lawsuit := actionQueryToNewLawsuit(req.Lawsuit)

	// Verify ALL the local trials for the requested stage
	respLocal, err := verifyLocalTrialsStage(tl, req.Stage, new_lawsuit, 2*time.Second)
	if err != nil {
		log.Printf("Error while verifying local trials (as aggregator DISTRICT) stage=%s: %v", req.Stage, err)
	}

	// If not found, return "none"
	if respLocal == nil || !respLocal.Success || respLocal.Match == "" || respLocal.Match == "none" {
		empty := TrialActionQueryResponse{
			Success: true,
			Stage:   req.Stage,
			Match:   "none",
			Message: "No corresponding lawsuit was found in this district.",
		}
		b, _ := json.Marshal(empty)
		_, _ = conn.WriteToUDP(b, remote)
		log.Printf("[DISTRICT->DISTRICT] %s - lawsuit_query stage=%s without correponding, returning 'none' for %s",
			time.Now().Format(time.RFC3339), req.Stage, remote.String())
		return
	}

	// Grants that district's name/ID are filled 
	if respLocal.DistrictName == "" || respLocal.DistrictID == 0 {
		districts := dl.GetAll()
		for _, d := range districts {
			if d.Name == nameDistrict {
				respLocal.DistrictID = d.ID
				respLocal.DistrictName = d.Name
				break
			}
		}
	}

	b, err := json.Marshal(respLocal)
	if err != nil {
		log.Printf("Error while coding response lawsuit_query (aggregator district): %v", err)
		return
	}

	if _, err := conn.WriteToUDP(b, remote); err != nil {
		log.Printf("Error while sending response lawsuit_query (aggregator district) to %s: %v", remote.String(), err)
		return
	}

	log.Printf("[DISTRICT->DISTRICT] %s - lawsuit_query stage=%s match=%s msg=%q to %s",
		time.Now().Format(time.RFC3339), respLocal.Stage, respLocal.Match, respLocal.Message, remote.String())
}


// ---------- District UDP server (for trials) ----------

func startTrialsServer(districtAddr, nameDistrict string, dl *DistrictList, tl *TrialList) {
	addr, err := net.ResolveUDPAddr("udp", districtAddr)
	if err != nil {
		log.Printf("Error while resolving district address (trials): %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("Error while opening UDP for trials at %s: %v", districtAddr, err)
		return
	}
	defer conn.Close()

	log.Printf("TRIALS server of district listening at %s", districtAddr)

	buf := make([]byte, 4096)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("Error while reading UDP of trial: %v", err)
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		// Detect the message type
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			log.Printf("Error while decoding the message type of the trial (%s): %v", remote.String(), err)
			continue
		}

		switch base.Type {
		case "trial_info":
			handleTrialInfo(conn, remote, data, nameDistrict, dl, tl)

		case "lawsuit_query":
			// request from OTHER DISTRICT for this district to verify
			// ALL its trials for the indicated stage
			handleActionQueryDistrict(conn, remote, data, nameDistrict, dl, tl)

		default:
			log.Printf("[DISTRICT] %s - unknown message type %q from %s",
				time.Now().Format(time.RFC3339), base.Type, remote.String())
		}

	}
}


// ---------- clear screen ----------
func clearScreen() {
	//fmt.Print("\033[2J\033[H")

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


// ---------- Simple structure for new lawsuit ----------
type NewLawsuit struct {
	Plaintiff  string
	Defendant  string
	CauseID    int
	Claims     []int
}

func newLawsuitToActionQuery(a NewLawsuit) ActionQuery {
	return ActionQuery{
		Plaintiff: a.Plaintiff,
		Defendant: a.Defendant,
		CauseID:   a.CauseID,
		Claims:    a.Claims,
	}
}

// Convert ActionQuery (used in messages) back to NewLawsuit 
func actionQueryToNewLawsuit(q ActionQuery) NewLawsuit {
	return NewLawsuit{
		Plaintiff: q.Plaintiff,
		Defendant: q.Defendant,
		CauseID:   q.CauseID,
		// make a copy of slice to avoid aliasing
		Claims: append([]int(nil), q.Claims...),
	}
}


// ---------- Aux functions for communication with TRIALS ----------

func verifyTrialStage(trialAddr string, stage string, lawsuit NewLawsuit, timeout time.Duration) (*TrialActionQueryResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", trialAddr)
	if err != nil {
		return nil, fmt.Errorf("error while resolving address for trial %s: %v", trialAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error while connecting in the trial %s: %v", trialAddr, err)
	}
	defer conn.Close()

	req := TrialActionQueryRequest{
		Type:  "lawsuit_query",
		Stage: stage,
		Lawsuit:  newLawsuitToActionQuery(lawsuit),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error while coding JSON for trial %s: %v", trialAddr, err)
	}

	log.Printf("[DISTRICT->TRIAL] %s - sending lawsuit_query stage=%s to %s",
		time.Now().Format(time.RFC3339), stage, trialAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("error while sending lawsuit_query to trial %s: %v", trialAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("error while receiving response from trial %s: %v", trialAddr, err)
	}

	var resp TrialActionQueryResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("error while decoding response of trial %s: %v", trialAddr, err)
	}

	log.Printf("[TRIAL->DISTRICT] %s - response stage=%s match=%s msg=%q of trial %s",
		time.Now().Format(time.RFC3339), resp.Stage, resp.Match, resp.Message, trialAddr)

	return &resp, nil
}

// it goes through all the trials of the local district, for deteminated stage/rule
// and returns the first positive response (res judicata, lis pendens, etc.)
func verifyLocalTrialsStage(tl *TrialList, stage string, lawsuit NewLawsuit, timeout time.Duration) (*TrialActionQueryResponse, error) {
	trials := tl.GetAll()
	for _, t := range trials {
		resp, err := verifyTrialStage(t.Address, stage, lawsuit, timeout)
		if err != nil {
			log.Printf("Warning: fault while verifying trial %s in the stage %s: %v", t.Address, stage, err)
			continue
		}
		if resp != nil && resp.Success && resp.Match != "" && resp.Match != "none" {
			// If the trial does not fullfill DistricName/DistrictID,
			// at least grants the address.
			if resp.TrialAddr == "" {
				resp.TrialAddr = t.Address
			}
			return resp, nil
		}
	}
	return nil, nil
}

// Verifies an address for DISTRICT (not trial) for a specific stage.
// The other district will treat this message as 'lawsuit_query' aggregating ALL
// its trias (through handleActionQueryDistrict).
func verifyDistrictStage(districtAddr string, stage string, lawsuit NewLawsuit, timeout time.Duration) (*TrialActionQueryResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", districtAddr)
	if err != nil {
		return nil, fmt.Errorf("error while resolving the address for district %s: %v", districtAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error while connecting to district %s: %v", districtAddr, err)
	}
	defer conn.Close()

	req := TrialActionQueryRequest{
		Type:  "lawsuit_query",
		Stage: stage,
		Lawsuit:  newLawsuitToActionQuery(lawsuit),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error while coding JSON for district %s: %v", districtAddr, err)
	}

	log.Printf("[DISTRICT->DISTRICT] %s - sending lawsuit_query stage=%s to %s",
		time.Now().Format(time.RFC3339), stage, districtAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("error while sending lawsuit_query to district %s: %v", districtAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("error while receving response from district %s: %v", districtAddr, err)
	}

	var resp TrialActionQueryResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("error while decoding response from district %s: %v", districtAddr, err)
	}

	log.Printf("[DISTRICT<-DISTRICT] %s - response stage=%s match=%s msg=%q from district %s",
		time.Now().Format(time.RFC3339), resp.Stage, resp.Match, resp.Message, districtAddr)

	return &resp, nil
}

// Travel ALL the OTHER districts (different than the local district) for one
// specific stage. Returns the first positive response (match != "" / "nome").
func verifyOtherDistrictsStage(
	nameDistrictLocal string,
	dl *DistrictList,
	stage string,
	lawsuit NewLawsuit,
	timeout time.Duration,
) (*TrialActionQueryResponse, error) {
	districts := dl.GetAll()
	for _, d := range districts {
		if strings.EqualFold(d.Name, nameDistrictLocal) {
			// jump the own district
			continue
		}
		districtAddr := strings.TrimSpace(d.Address)
		if districtAddr == "" {
			continue
		}

		resp, err := verifyDistrictStage(districtAddr, stage, lawsuit, timeout)
		if err != nil {
			log.Printf("Warning: fault while verifying district %s (%s) in the stage %s: %v",
				d.Name, districtAddr, stage, err)
			continue
		}
		if resp != nil && resp.Success && resp.Match != "" && resp.Match != "none" {
			// Grants district's info, if came empty
			if resp.DistrictID == 0 {
				resp.DistrictID = d.ID
			}
			if resp.DistrictName == "" {
				resp.DistrictName = d.Name
			}
			return resp, nil
		}
	}
	return nil, nil
}

// Send request to create a lawsuit for a specific trial 
func createLawsuitInTrialAddr(trialAddr, reason, related string, lawsuit NewLawsuit, timeout time.Duration) (*TrialCreateActionResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", trialAddr)
	if err != nil {
		return nil, fmt.Errorf("error while resolving address for trial %s: %v", trialAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error while connecting to the trial %s: %v", trialAddr, err)
	}
	defer conn.Close()

	req := TrialCreateActionRequest{
		Type:    "lawsuit_create",
		Reason:  reason,
		Lawsuit: newLawsuitToActionQuery(lawsuit),
		Related: related,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error while decoding JSON (lawsuit_create) to trial %s: %v", trialAddr, err)
	}

	log.Printf("[DISTRICT->TRIAL] %s - sending lawsuit_create reason=%s to %s (related=%s)",
		time.Now().Format(time.RFC3339), reason, trialAddr, related)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("error while sending lawsuit_create to trial %s: %v", trialAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("error while receiving response from lawsuit_create of trial %s: %v", trialAddr, err)
	}

	var resp TrialCreateActionResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("error while decoding response lawsuit_create from trial %s: %v", trialAddr, err)
	}

	log.Printf("[TRIAL->DISTRICT] %s - response lawsuit_create success=%v lawsuit_id=%s msg=%q (trial=%s)",
		time.Now().Format(time.RFC3339), resp.Success, resp.LawsuitID, resp.Message, trialAddr)

	return &resp, nil
}

// Send request to merge claims in lawsuit already existent (containment)
func sendMergeClaimsToTrialAddr(trialAddr, lawsuitID string, newClaims []int, timeout time.Duration) (*TrialMergeClaimsResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", trialAddr)
	if err != nil {
		return nil, fmt.Errorf("error while resolving address for trial %s: %v", trialAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error while connecting in the trial %s: %v", trialAddr, err)
	}
	defer conn.Close()

	req := TrialMergeClaimsRequest{
		Type:      "lawsuit_merge_claims",
		LawsuitID: lawsuitID,
		NewClaims: newClaims,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error while coding JSON (lawsuit_merge_claims) to trial %s: %v", trialAddr, err)
	}

	log.Printf("[DISTRICT->TRIAL] %s - sending lawsuit_merge_claims lawsuit_id=%s to %s",
		time.Now().Format(time.RFC3339), lawsuitID, trialAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("error while sending lawsuit_merge_claims to trial %s: %v", trialAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("error while receiving response lawsuit_merge_claims from trial %s: %v", trialAddr, err)
	}

	var resp TrialMergeClaimsResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("error while decoding response lawsuit_merge_claims from trial %s: %v", trialAddr, err)
	}

	log.Printf("[TRIAL->DISTRICT] %s - response lawsuit_merge_claims success=%v msg=%q (trial=%s)",
		time.Now().Format(time.RFC3339), resp.Success, resp.Message, trialAddr)

	return &resp, nil
}

// ---------- NEW: Function to send search request to a trial ----------
func searchLawsuitsAtTrial(trialAddr, field, value string, timeout time.Duration) (*TrialSearchLawsuitsResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", trialAddr)
	if err != nil {
		return nil, fmt.Errorf("error while resolving address for trial %s: %v", trialAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error while connecting in the trial %s: %v", trialAddr, err)
	}
	defer conn.Close()

	req := TrialSearchLawsuitsRequest{
		Type:  "search_lawsuit",
		Field: field,
		Value: value,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error while coding JSON (search_lawsuit) for trial %s: %v", trialAddr, err)
	}

	log.Printf("[DISTRICT->TRIAL] %s - sending search_lawsuit field=%s value=%q to %s",
		time.Now().Format(time.RFC3339), field, value, trialAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("error while sending search_lawsuit to trial %s: %v", trialAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65535)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("error while receiving response search_lawsuit from trial %s: %v", trialAddr, err)
	}

	var resp TrialSearchLawsuitsResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("error while decoding response search_lawsuit from trial %s: %v", trialAddr, err)
	}

	log.Printf("[TRIAL->DISTRICT] %s - response search_lawsuit success=%v results=%d msg=%q (trial=%s)",
		time.Now().Format(time.RFC3339), resp.Success, len(resp.Results), resp.Message, trialAddr)

	return &resp, nil
}

// Verify the workload (actives lawsuits) for a specific trial
func verifyWorkloadTrial(trialAddr string, timeout time.Duration) (int, error) {
	addr, err := net.ResolveUDPAddr("udp", trialAddr)
	if err != nil {
		return 0, fmt.Errorf("error while resolving the address for trial %s: %v", trialAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return 0, fmt.Errorf("error while connecting in the trial %s: %v", trialAddr, err)
	}
	defer conn.Close()

	req := TrialWorkloadRequest{Type: "workload_info"}
	data, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("error while coding JSON (workload_info) for trial %s: %v", trialAddr, err)
	}

	log.Printf("[DISTRICT->TRIAL] %s - sending workload_info to %s",
		time.Now().Format(time.RFC3339), trialAddr)

	if _, err := conn.Write(data); err != nil {
		return 0, fmt.Errorf("error while sending workload_info to trial %s: %v", trialAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return 0, fmt.Errorf("error while receiving workload response for trial %s: %v", trialAddr, err)
	}

	var resp TrialWorkloadResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return 0, fmt.Errorf("error while decoding workload response for trial %s: %v", trialAddr, err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("trial %s fault response in the workload verification: %s", trialAddr, resp.Message)
	}

	return resp.ActiveWorkload, nil
}


// ---------- FREE Distribution (rule 6) ----------

func lawsuitFreeDistribution(nameDistrict string, tl *TrialList, lawsuit NewLawsuit, timeout time.Duration) (string, error) {
	trials := tl.GetAll()
	if len(trials) == 0 {
		return "", fmt.Errorf("no registered trials in this district")
	}

	// Choose the trial with SMALL workload (fewer number of active lawsuits)
	var (
		bestTrial    Trial
		bestWorkload int
		found        bool
	)

	for _, t := range trials {
		workload, err := verifyWorkloadTrial(t.Address, timeout)
		if err != nil {
			log.Printf("Warning: fault while getting the workload for the trial %s: %v", t.Address, err)
			continue
		}
		if !found || workload < bestWorkload {
			found = true
			bestWorkload = workload 
			bestTrial = t
		}
	}

	// If not possible to get the workload for the trials, goes to random fallback 
	if !found {
		rand.Seed(time.Now().UnixNano())
		bestTrial = trials[rand.Intn(len(trials))]
		log.Printf("Free distribution: workload not get; choosing a random trial: %s", bestTrial.Address)
	} else {
		log.Printf("Free distribution: choosing trial %s with workload %d", bestTrial.Address, bestWorkload)
	}

	createResp, err := createLawsuitInTrialAddr(bestTrial.Address, "free", "", lawsuit, timeout)
	if err != nil {
		return "", fmt.Errorf("error while creating lawsuit with free distribution at trial %s: %v", bestTrial.Address, err)
	}
	if !createResp.Success {
		return "", fmt.Errorf("trial refused create lawsuit by free distribution: %s", createResp.Message)
	}

	lawsuitID := createResp.LawsuitID
	if lawsuitID == "" {
		lawsuitID = "(ID not returned by trial)"
	}

	msg := fmt.Sprintf(
		"FREE DISTRIBUTION.\n\nDistrict: %s\nTrial: ID %d (address %s)\nIdentification for the created lawsuit: %s\n\nPlaintiff: %s\nDefendant: %s\nCause (ID): %d\nClaims (IDs): %v\n",
		strings.ToUpper(nameDistrict),
		createResp.TrialID, bestTrial.Address,
		lawsuitID,
		lawsuit.Plaintiff, lawsuit.Defendant, lawsuit.CauseID, lawsuit.Claims,
	)

	if found {
		msg += fmt.Sprintf("\nCriteria: trial with small workload (active lawsuits= %d) in the district.\n", bestWorkload)
	} else {
		msg += "\nCriteria: not possible to get the workload for the trials; used random choice.\n"
	}

	return msg, nil
}


// ---------- Parser for the claims (IDs separated by commas) ----------

func parseClaimsInput(input string) ([]int, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("claim not informed")
	}
	parties := strings.Split(s, ",")
	var claims []int
	for _, p := range parties {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid claim: %q (integer expected)", p)
		}
		claims = append(claims, id)
	}
	if len(claims) == 0 {
		return nil, fmt.Errorf("no valid claim informed")
	}
	return claims, nil
}


// ---------- Interactive Menu ----------

func main() {
	// Flags
	helpFlag := flag.Bool("h", false, "Show help")
	infoFlag := flag.Bool("info", false, "Show information about option flags")
	nameFlag := flag.String("name", "", "District name (if empty, uses the name saved in file district_name.txt)")
	courtAddr := flag.String("court", "127.0.0.1:9000", "Court's UDP address")
	addrFlag := flag.String("addr", "", "UDP address for this district (for trials). If empty, uses information in the file district_addr.txt or search in the Court.")
	districtsFile := flag.String("districts", "districts_local.json", "Districts' local file")
	trialsFile := flag.String("trials", "trials.json", "Trials' local file")
	logFlag := flag.String("log", "", "Log file (or 'term' for log in the terminal; default: district.log)")
	flag.Parse()

	if *helpFlag {
		fmt.Println("Program used to simulate the descentralization of the procedure for adding") 
		fmt.Println("a new lawsuit in one of the existing trials in one of the various judicial districts")
		fmt.Println("of the Justice Court of São Paulo (Tribunal de Justiça de São Paulo), in Brazil.")
		fmt.Println("\n Release:", Release)
		fmt.Println()
		fmt.Println("Usage: district [-h] [-info] [-addr <UDP address>] [-court <UDP address>] [-name <district name>] [-log <file_name|term>]")
		fmt.Println("       at least -name option must be given if there isn't the file district_name.txt at current folder")
		return
	}

	// Uses -info as the default behavior for -h
	if *infoFlag {
		flag.Usage()
		os.Exit(0)
	}

	// 1) Resolves district's NAME
	nameFromFile := loadDistrictName(nameDistrictFile)
	nameDistrict := strings.TrimSpace(*nameFlag)

	if nameDistrict == "" {
		if nameFromFile == "" {
			fmt.Println("Error: district's name not informed by -name or found in file district_name.txt.\n")
			flag.Usage()
			os.Exit(1)
		}
		nameDistrict = nameFromFile
	}

	if nameDistrict != nameFromFile {
		saveNameDistrict(nameDistrictFile, nameDistrict)
	}

	// LOG Configuration (if a valid district's name)
	if *logFlag == "" {
		logFile, err := os.OpenFile("district.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error while opening default log file (district.log):", err)
		} else {
			log.SetOutput(logFile)
		}
	} else if *logFlag == "term" {
		// stay with default output (stderr)
	} else {
		logFile, err := os.OpenFile(*logFlag,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error while opening log file:", err)
		} else {
			log.SetOutput(logFile)
		}
	}

	// Local districts' list
	dl := NewDistrictList(*districtsFile)
	if err := dl.Load(); err != nil {
		log.Printf("Error while loading local districts: %v", err)
	}

	// 2) Resolves district's ADDRESS
	districtAddr := strings.TrimSpace(*addrFlag)
	if districtAddr == "" {
		addrFromFile := loadDistrictAddress(addrDistrictFile)
		if addrFromFile != "" {
			districtAddr = addrFromFile
		} else {
			log.Printf("District's address was neither provided nor found in file. Trying to get it from the Court for the district %q...", nameDistrict)
			if err := updateDistrictsOfCourt(*courtAddr, dl); err != nil {
				log.Printf("Error while trying to get the list of districts from the Court: %v", err)
			} else {
				districts := dl.GetAll()
				for _, d := range districts {
					if d.Name == nameDistrict {
						districtAddr = strings.TrimSpace(d.Address)
						if districtAddr != "" {
							break
						}
					}
				}
			}

			if districtAddr == "" {
				fmt.Println("Error: it was not possible to set the UDP address for the district.")
				fmt.Println("Enter it by the flag -addr or configure the file", addrDistrictFile, "(the Court must be running if not used -addr flag).")
				os.Exit(1)
			}
		}
	}

	addrFromFile := loadDistrictAddress(addrDistrictFile)
	if districtAddr != addrFromFile {
		saveAddressDistrict(addrDistrictFile, districtAddr)
	}

	log.Printf("Starting DISTRICT %q. Court in %s. District listening trials in %s.",
		nameDistrict, *courtAddr, districtAddr)

	// Updates districts of Court (best effort)
	if err := updateDistrictsOfCourt(*courtAddr, dl); err != nil {
		log.Printf("It was not possible to update the districts from the Court: %v", err)
		log.Printf("Using local list (if existent).")
	}

	// Trials' local list
	tl := NewTrialList(*trialsFile)
	if err := tl.Load(); err != nil {
		log.Printf("Error while loading local trials: %v", err)
	}

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Printf("DISTRICT %q. Court in %s. District listening trials in %s.",
		nameDistrict, *courtAddr, districtAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()


	// UDP server for trials (now with access to the list of districts/trial and district's name)
	go startTrialsServer(districtAddr, nameDistrict, dl, tl)


	// Interactive Menu
	reader := bufio.NewReader(os.Stdin)
	const udpTimeout = 2 * time.Second

	for {
		fmt.Printf("\n========== DISTRICT - %s ==========\n", strings.ToUpper(nameDistrict))
		fmt.Println("1 (E) - Enter a lawsuit")
		fmt.Println("2 (S) - Search for lawsuits")
		fmt.Println("3 (D) - List the districts")
		fmt.Println("4 (T) - List the trials")
		fmt.Println("5 (A) - Add a trial")
		fmt.Println("6 (M) - Remove a trial")
		fmt.Println("7 (Q) - Quit")
		fmt.Println("8 (R) - Refresh (clear the screen)")
		fmt.Print("Your option> ")

		line, _ := reader.ReadString('\n')
		opt := strings.TrimSpace(line)

		switch opt {

		case "8", "r", "R":
			clearScreen()
			continue

		case "1", "E", "e":
			// 1) Try to update the districts' list in the Court
			fmt.Println("\nUpdating the districts' list in the Court...")
			if err := updateDistrictsOfCourt(*courtAddr, dl); err != nil {
				fmt.Println("Warning: it was not possible to connect to the Court. Using local list.")
				log.Printf("Fault while updating the Court's districts before adding a new lawsuit: %v", err)
			} else {
				fmt.Println("Districts' list updated from the Court.")
			}

			// 2) Ask for new lawsuit data 
			fmt.Print("\nPlaintiff: ")
			plaintiff, _ := reader.ReadString('\n')
			plaintiff = strings.TrimSpace(plaintiff)

			fmt.Print("Defendant: ")
			defendant, _ := reader.ReadString('\n')
			defendant = strings.TrimSpace(defendant)

			fmt.Print("Cause of action (numeric ID): ")
			causeStr, _ := reader.ReadString('\n')
			causeStr = strings.TrimSpace(causeStr)
			causeID, err := strconv.Atoi(causeStr)
			if err != nil || causeID <= 0 {
				fmt.Println("Invalid cause of action (must be an integer).")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Claims (numeric IDs separated by commas; ex.: 10 or 10,20,30): ")
			pedStr, _ := reader.ReadString('\n')
			pedStr = strings.TrimSpace(pedStr)
			claims, err := parseClaimsInput(pedStr)
			if err != nil {
				fmt.Println("Error:", err)
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			new_lawsuit := NewLawsuit{
				Plaintiff: plaintiff,
				Defendant: defendant,
				CauseID:   causeID,
				Claims:    claims,
			}

			fmt.Println("\nStarting the verification for the lawsuit distribution...")
			fmt.Println("1) Res judicata")
			// 1) RES JUDICATA 
			respRJ, err := verifyLocalTrialsStage(tl, "res_judicata", new_lawsuit, udpTimeout)
			if err == nil && respRJ != nil && respRJ.Match == "res_judicata" {
				fmt.Println("\n*** RES JUDICATA	***")
				fmt.Println("It was found an identical lawsuit (same plaintiff, defendant, cause of action and claims) of already judged lawsuit WITH merits resolution.")
				fmt.Printf("District: %s\n", respRJ.DistrictName)
				fmt.Printf("Trial: ID %d (%s)\n", respRJ.TrialID, respRJ.TrialAddr)
				fmt.Printf("Lawsuit identification: %s\n", respRJ.LawsuitID)
				fmt.Println("It is not possible to create a new identical lawsuit, because there is already final judgment.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			// If not found locally, search in the OTHERS districts
			if respRJ == nil || !respRJ.Success || respRJ.Match == "" || respRJ.Match == "none" {
				respRJ, err = verifyOtherDistrictsStage(nameDistrict, dl, "res_judicata", new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Warning: error while verifying other districts for RES JUDICATA:", err)
				}
			}

			if respRJ != nil && respRJ.Success && respRJ.Match == "res_judicata" {
				fmt.Println("\n*** RES JUDICATA ***")
				fmt.Println("It was found identical lawsuit (same plaintiff, defendant, cause of action and claims) already judged WITH merits resolution.")
				fmt.Printf("District: %s (ID %d)\n", respRJ.DistrictName, respRJ.DistrictID)
				fmt.Printf("Trial: ID %d (%s)\n", respRJ.TrialID, respRJ.TrialAddr)
				fmt.Printf("Lawsuit identification: %s\n", respRJ.LawsuitID)
				fmt.Println("It is not possible to create a new identical lawsuit, because there is already final judgment.")

				fmt.Print("\nPress ENTER to return to menu...")
				bufio.NewReader(os.Stdin).ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Warning: fault while verifying res judicata in the local trials:", err)
			}

			fmt.Println("2) Lis pendens")
			// 2) LIS PENDENS
			respLit, err := verifyLocalTrialsStage(tl, "lis_pendens", new_lawsuit, udpTimeout)

			// If nout found locally, search in the OTHERS districts
			if respLit == nil || !respLit.Success || respLit.Match == "" || respLit.Match == "none" {
				respLit, err = verifyOtherDistrictsStage(nameDistrict, dl, "lis_pendens", new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Warning: error while verifying others districts for LIS PENDENS:", err)
				}
			}

			if respLit != nil && respLit.Success && respLit.Match == "lis_pendens" {
				fmt.Println("\n*** LIS PENDENS ***")
				fmt.Println("It was found identical lawsuit (same plaintiff, defendant, cause of action and claims) in the ACTIVE lawsuits list.")
				fmt.Printf("District: %s\n", respLit.DistrictName)
				fmt.Printf("Trial: ID %d (%s)\n", respLit.TrialID, respLit.TrialAddr)
				fmt.Printf("Identification of active lawsuit: %s\n", respLit.LawsuitID)
				fmt.Println("A new lawsuit will not be created, because it is case of lis pendens.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Warning: fault while verifying lis pendens in the local trials:", err)
			}

			fmt.Println("3) Repeated request (judged WITHOUT merits resolution)")
			// 3) REPEATED REQUEST 
			respRR, err := verifyLocalTrialsStage(tl, "repeated_request", new_lawsuit, udpTimeout)

			// If not found locally, search in the OTHERS districts 
			if respRR == nil || !respRR.Success || respRR.Match == "" || respRR.Match == "none" {
				respRR, err = verifyOtherDistrictsStage(nameDistrict, dl, "repeated_request", new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Warning: error while verifying others districts for REPEATED REQUEST:", err)
				}
			}

			if respRR != nil && respRR.Success && respRR.Match == "repeated_request" {
				fmt.Println("\n*** REPEATED REQUEST ***")
				fmt.Println("Its was found identical lawsuit in the lawsuits judged WITHOUT merits resolution.")
				fmt.Printf("District: %s\n", respRR.DistrictName)
				fmt.Printf("Trial: ID %d (%s)\n", respRR.TrialID, respRR.TrialAddr)
				fmt.Printf("Identification for the already judged lawsuit: %s\n", respRR.LawsuitID)
				fmt.Println("A new lawsuit will be created (new sequential number) in the SAME trial where take place the jugement without merits resolution.")

				createResp, err := createLawsuitInTrialAddr(respRR.TrialAddr, "repeated_request", respRR.LawsuitID, new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Error while creating lawsuit due repetead request:", err)
				} else if !createResp.Success {
					fmt.Println("Trial refused the lawsuit creation due repeated request:", createResp.Message)
				} else {
					fmt.Printf("\nNew lawsuit created as REPEATED REQUEST.\nIdentification for the new lawsuit: %s\n", createResp.LawsuitID)
				}

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Warning: fault while verifying repeated request in the local trials:", err)
			}

			fmt.Println("4) Joinder")
			// 4) JOINDER (CONTAINMENT)
			respCont, err := verifyLocalTrialsStage(tl, "joinder", new_lawsuit, udpTimeout)

			// If not found locally, verify OTHERS districts
			if respCont == nil || !respCont.Success || respCont.Match == "" || respCont.Match == "none" {
				respCont, err = verifyOtherDistrictsStage(nameDistrict, dl, "joinder", new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Warning: error while verifying others districts for JOINDER:", err)
				}
			}

			if respCont != nil && respCont.Success && (respCont.Match == "joinder_contained" || respCont.Match == "joinder_continent") {
				if respCont.Match == "joinder_contained" {
					fmt.Println("\n*** JOINDER (CONTAINED LAWSUIT) ***")
					fmt.Println("It was found CONTINENT lawsuit (bigger claim) with same parties and same cause of action.")
					fmt.Printf("District: %s\n", respCont.DistrictName)
					fmt.Printf("Trial: ID %d (%s)\n", respCont.TrialID, respCont.TrialAddr)
					fmt.Printf("Identification of CONTINENT lawsuit: %s\n", respCont.LawsuitID)
					fmt.Println("A new lawsuit will not be created because the new lawsuit's claim is CONTAINED in the CONTINENT lawsuit.")
				} else if respCont.Match == "joinder_continent" {
					fmt.Println("\n*** JOINDER (CONTINENT LAWSUIT) ***")
					fmt.Println("It was found a CONTAINED lawsuit (lower claim) with same parties and same cause of action.")
					fmt.Printf("District: %s\n", respCont.DistrictName)
					fmt.Printf("Trial: ID %d (%s)\n", respCont.TrialID, respCont.TrialAddr)
					fmt.Printf("Identification of CONTAINED lawsuit (to be expanded): %s\n", respCont.LawsuitID)
					fmt.Println("The lawsuits will be CONSOLIDATED, adding the new lawsuit claims to the list of claims for the CONTINENT lawsuit.")

					_, err := sendMergeClaimsToTrialAddr(respCont.TrialAddr, respCont.LawsuitID, new_lawsuit.Claims, udpTimeout)
					if err != nil {
						fmt.Println("Error while sending merge of claims to the trial:", err)
					} else {
						fmt.Println("New lawsuit's claims sent to be consolidated at new CONTINENT lawsuit (old CONTAINED lawsuit).")
					}
				}

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Warning: fault while verifying joinder in the local trials:", err)
			}

			fmt.Println("5) Connection")
			// 5) CONNECTION
			respConx, err := verifyLocalTrialsStage(tl, "connection", new_lawsuit, udpTimeout)

			// If not found locally, verify OTHERS districts
			if respConx == nil || !respConx.Success || respConx.Match == "" || respConx.Match == "none" {
				respConx, err = verifyOtherDistrictsStage(nameDistrict, dl, "connection", new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Warning: error while verifying other districts for CONNECTION:", err)
				}
			}

			if respConx != nil && respConx.Success && respConx.Match == "connection" {
				fmt.Println("\n*** CONNECTION ***")
				fmt.Println("It was found CONNECTED lawsuit (same cause of action and/or same claims).")
				fmt.Printf("District: %s\n", respConx.DistrictName)
				fmt.Printf("Trial: ID %d (%s)\n", respConx.TrialID, respConx.TrialAddr)
				fmt.Printf("Identification of already existent lawsuit: %s\n", respConx.LawsuitID)
				fmt.Println("The new lawsuit will be created in the SAME trial, for joint judgment (due the connection).")

				createResp, err := createLawsuitInTrialAddr(respConx.TrialAddr, "connection", respConx.LawsuitID, new_lawsuit, udpTimeout)
				if err != nil {
					fmt.Println("Error while creating lawsuit by connection:", err)
				} else if !createResp.Success {
					fmt.Println("Trial refused to create lawsuit by connection:", createResp.Message)
				} else {
					fmt.Printf("\nNew lawsuit created as CONNECTED.\nIdentification of the new lawsuit: %s\n", createResp.LawsuitID)
					fmt.Println("The trial (server side) must internally register the connection between the connected lawsuits for joint judgment.")
				}

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Warning: fault while verifying connection in the local trials:", err)
			}

			fmt.Println("6) FREE Distribution")
			// 6) FREE DISTRIBUTION
			msg, err := lawsuitFreeDistribution(nameDistrict, tl, new_lawsuit, udpTimeout)
			if err != nil {
				fmt.Println("Error while doing a free distribution:", err)
			} else {
				fmt.Println()
				fmt.Println(msg)
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "2", "S", "s":
			// ---------- SEARCH FOR LAWSUITS IN ALL DISTRICT'S TRIALS ----------
			trials := tl.GetAll()
			if len(trials) == 0 {
				fmt.Println("There are no trials registered in this district.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			clearScreen()
			fmt.Println()
			fmt.Println("Search for lawsuits in ALL the trials of this district.")
			fmt.Println("Buscar por:")
			fmt.Println("Search for:")
			fmt.Println("1 (I) - Lawsuit ID")
			fmt.Println("2 (P) - Plaintiff")
			fmt.Println("3 (D) - Defendant")
			fmt.Println("4 (C) - Cause of action (exact number)")
			fmt.Println("5 (M) - Claim (exact number)")
			fmt.Println("6 (R) - Return to  menu")
			fmt.Print("Your option> ")
			fieldStr, _ := reader.ReadString('\n')
			fieldStr = strings.TrimSpace(fieldStr)

			var field string
			switch fieldStr {
			case "1", "I", "i":
				field = "id"
			case "2", "P", "p":
				field = "plaintiff"
			case "3", "D", "d":
				field = "defendant"
			case "4", "C", "c":
				field = "cause"
			case "5", "M", "m":
				field = "claim"
			case "6", "R", "r":
				clearScreen()
				continue
			default:
				fmt.Println("Invalid option.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Value for serach> ")
			val, _ := reader.ReadString('\n')
			val = strings.TrimSpace(val)
			if val == "" {
				fmt.Println("Empty search value.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Println("\nSearching in all trials of this district...")
			totalFound := 0

			for _, t := range trials {
				resp, err := searchLawsuitsAtTrial(t.Address, field, val, udpTimeout)
				if err != nil {
					fmt.Printf("Warning: fault while searching in the Trial ID %d (%s): %v\n", t.ID, t.Address, err)
					continue
				}
				if !resp.Success {
					fmt.Printf("Warning: Trial ID %d (%s) returned error: %s\n", t.ID, t.Address, resp.Message)
					continue
				}
				if len(resp.Results) == 0 {
					continue
				}

				trialID := resp.TrialID
				trialAddr := resp.TrialAddr
				if trialID == 0 {
					trialID = t.ID
				}
				if trialAddr == "" {
					trialAddr = t.Address
				}

				for _, r := range resp.Results {
					if totalFound == 0 {
						fmt.Println("\n--- SEARCH RESULTS ---")
					}
					totalFound++
					fmt.Printf("[Trial %d - %s] [%s] ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v\n",
						trialID, trialAddr,
						r.List,
						r.ID, r.Plaintiff, r.Defendant, r.CauseAction, r.Claims)
				}
			}

			if totalFound == 0 {
				fmt.Println("no lawsuit found in this district's trials.")
			} else {
				fmt.Printf("\nTotal of found lawsuits: %d\n", totalFound)
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "3", "D", "d":
			fmt.Println("\nSearching districts' list in the Court...")
			err := updateDistrictsOfCourt(*courtAddr, dl)
			if err != nil {
				fmt.Println("It was not possible to connect to the Court. Using local list.")
				log.Printf("Fault while updating the Court's districts: %v", err)
			} else {
				fmt.Println("Districts list updated from the Court.")
			}

			districts := dl.GetAll()
			if len(districts) == 0 {
				fmt.Println("(none district in the list)")
			} else {
				fmt.Println("\n--- DISTRICTS ---")
				for _, d := range districts {
					fmt.Printf("ID %d | %s | %s | %d trials\n",
						d.ID, d.Name, d.Address, d.Trials)
				}
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "4", "T", "t":
			trials := tl.GetAll()
			if len(trials) == 0 {
				fmt.Println("(no trials registered for this district)")
			} else {
				fmt.Println("\n--- TRIALS ---")
				for _, t := range trials {
					fmt.Printf("ID %d | Endereço UDP: %s\n", t.ID, t.Address)
				}
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "5", "A", "a":
			fmt.Print("UDP address for the new trial (ex: 127.0.0.1:9201): ")
			endStr, _ := reader.ReadString('\n')
			endStr = strings.TrimSpace(endStr)
			if endStr == "" {
				fmt.Println("Invalid address.")

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			t, err := tl.Add(endStr)
			if err != nil {
				fmt.Println("Error while adding trial:", err)
				log.Printf("Error while adding trial: %v", err)

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}
			fmt.Println()
			fmt.Printf("Trial added: ID %d, address %s\n", t.ID, t.Address)

			totalTrials := tl.Count()
			if err := sendUpdateTrials(*courtAddr, nameDistrict, totalTrials); err != nil {
				fmt.Println("Warning: it was not possible notify the Court about the number of trials.")
				log.Printf("Error while sending update_trials to the Court: %v", err)
			} else {
				fmt.Println("Court notified about the number of trials.")
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "6", "M", "m":
			fmt.Print("ID da trial to be removed: ")
			idStr, _ := reader.ReadString('\n')
			idStr = strings.TrimSpace(idStr)
			id, err := strconv.Atoi(idStr)
			if err != nil {
				fmt.Println("Invalid ID.")

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			t, err := tl.RemoveByID(id)
			if err != nil {
				fmt.Println("Error while removing trial:", err)
				log.Printf("Error while removing trial: %v", err)

				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}
			fmt.Println()
			fmt.Printf("Trial removed: ID %d, address %s\n", t.ID, t.Address)

			totalTrials := tl.Count()
			if err := sendUpdateTrials(*courtAddr, nameDistrict, totalTrials); err != nil {
				fmt.Println("Warning: it was not possible notify the Court about the number of trials.")
				log.Printf("Error while sending update_trials to the Court: %v", err)
			} else {
				fmt.Println("Court notified about the new number of trials.")
			}

			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()

		case "7", "Q", "q":
			// Quit
			if err := tl.Save(); err != nil {
				log.Printf("Error while saving trials during quit: %v", err)
			}
			if err := tl.Save(); err != nil {
				log.Printf("Error while saving trials during quit: %v", err)
			}
			saveNameDistrict(nameDistrictFile, nameDistrict)
			saveAddressDistrict(addrDistrictFile, districtAddr)
			fmt.Println("Data saved. Finishing district.")
			return

		default:
			fmt.Println("Invalid option.")
			fmt.Print("\nPress ENTER to return to menu...")
			reader.ReadString('\n')
			clearScreen()
		}
	}
}
