/***************************************************************************
        Distributed Architecture for Judiciary Processes Distribution
        ===== Trial Agent ====

        Authors:
                Antonio Gilberto de Moura (A - AGM)
                Fernado Maurício Gomes (F - FMG)

        Rel 1.1.0


Revision History for court.go:

   Release   Author   Date           Description
    1.0.0    A/F      19/NoV/2025    Initial stable release
    1.1.0    A        28/Jan/2026    Translation to English

***************************************************************************/

package main

import (
	"bufio"
	"encoding/json"
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
const Release = "1.1.0"  // Translation to English


// ---------- Data Structures ----------

// Lawsuit with ID "ID_District.ID_Trial.Sequence"
// Now with claims list (Claims []int) and possible connected lawsuits list.
// ClaimLegacy necessary only for read old files (where there was only one "claim" int)
// and it is clean before saving again.
type Lawsuit struct {
	ID          string   `json:"id"`
	Plaintiff   string   `json:"plaintiff"`
	Defendant   string   `json:"defendant"`
	CauseAction int      `json:"cause_action"`
	Claims      []int    `json:"claims,omitempty"`
	Connected   []string `json:"connected,omitempty"`

	// Legacy field for migration of old files (where there was only one int "claim").
	ClaimLegacy int      `json:"claim,omitempty"`
}

// Complete status for the trail (persistence in JSON)
type TrialState struct {
	DistrictID              int       `json:"district_id"`
	DistrictName            string    `json:"district_name"`
	TrialID                 int       `json:"trial_id"`
	TrialAddr               string    `json:"trial_addr"`
	NextSeq                 int       `json:"next_seq"`
	ActivesLawsuits         []Lawsuit `json:"actives_lawsuits"`
	LawsuitsDisWithMerit    []Lawsuit `json:"lawsuits_dismissed_with_merit"`
	LawsuitsDisWithoutMerit []Lawsuit `json:"lawsuits_dismissed_without_merit"`
}

// Wrapper with mutex + file path
type TrialStore struct {
	mu       sync.RWMutex
	state    TrialState
	filePath string
}

// Creates a new store with file (IDs will be filled by handshake / mirror) 
func NewTrialStore(filePath string) *TrialStore {
	return &TrialStore{
		state: TrialState{
			DistrictID:              0,
			DistrictName:            "",
			TrialID:                 0,
			TrialAddr:               "",
			NextSeq:                 1,
			ActivesLawsuits:         []Lawsuit{},
			LawsuitsDisWithMerit:    []Lawsuit{},
			LawsuitsDisWithoutMerit: []Lawsuit{},
		},
		filePath: filePath,
	}
}

// Lawsuits migration with legacy field "claim" -> "claims"
func migrateLegacyClaims(a *Lawsuit) {
	if len(a.Claims) == 0 && a.ClaimLegacy != 0 {
		a.Claims = []int{a.ClaimLegacy}
		a.ClaimLegacy = 0
	}
}

func (ts *TrialStore) Load() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	f, err := os.Open(ts.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var st TrialState
	if err := dec.Decode(&st); err != nil {
		return err
	}

	// Legacy claims migration
	for i := range st.ActivesLawsuits {
		migrateLegacyClaims(&st.ActivesLawsuits[i])
	}
	for i := range st.LawsuitsDisWithMerit {
		migrateLegacyClaims(&st.LawsuitsDisWithMerit[i])
	}
	for i := range st.LawsuitsDisWithoutMerit {
		migrateLegacyClaims(&st.LawsuitsDisWithoutMerit[i])
	}

	if st.NextSeq <= 0 {
		st.NextSeq = 1
	}
	ts.state = st
	return nil
}

func (ts *TrialStore) Save() error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	tmp := ts.filePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ts.state); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, ts.filePath)
}

func (ts *TrialStore) saveLocked() error {
	tmp := ts.filePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ts.state); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, ts.filePath)
}

func (ts *TrialStore) nextID() string {
	seq := ts.state.NextSeq
	ts.state.NextSeq++
	return fmt.Sprintf("%d.%d.%d", ts.state.DistrictID, ts.state.TrialID, seq)
}

// Creates a new ACTIVE lawsuit (with claims' list and possible connected list)
func (ts *TrialStore) CreateLawsuit(plaintiff, defendant string, cause int, claims []int, connected []string) (Lawsuit, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	id := ts.nextID()
	a := Lawsuit{
		ID:          id,
		Plaintiff:   plaintiff,
		Defendant:   defendant,
		CauseAction: cause,
		Claims:      append([]int(nil), claims...),
		Connected:   append([]string(nil), connected...),
	}
	ts.state.ActivesLawsuits = append(ts.state.ActivesLawsuits, a)

	if err := ts.saveLocked(); err != nil {
		return Lawsuit{}, err
	}
	return a, nil
}

// Dismiss the lawsuit (active -> dismissed WITH merit)
func (ts *TrialStore) DismissWithMerit(id string) (Lawsuit, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	idx := -1
	var a Lawsuit
	for i, ac := range ts.state.ActivesLawsuits {
		if ac.ID == id {
			idx = i
			a = ac
			break
		}
	}
	if idx == -1 {
		return Lawsuit{}, fmt.Errorf("lawsuit %q not found in the actives lawsuits list", id)
	}

	ts.state.ActivesLawsuits = append(ts.state.ActivesLawsuits[:idx], ts.state.ActivesLawsuits[idx+1:]...)
	ts.state.LawsuitsDisWithMerit = append(ts.state.LawsuitsDisWithMerit, a)

	if err := ts.saveLocked(); err != nil {
		return Lawsuit{}, err
	}
	return a, nil
}

// Dismiss the lawsuit (active -> dismissed WITHOUT merit)
func (ts *TrialStore) DismissWithoutmerit(id string) (Lawsuit, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	idx := -1
	var a Lawsuit
	for i, ac := range ts.state.ActivesLawsuits {
		if ac.ID == id {
			idx = i
			a = ac
			break
		}
	}
	if idx == -1 {
		return Lawsuit{}, fmt.Errorf("lawsuit %q not found in the actives lawsuits list", id)
	}

	ts.state.ActivesLawsuits = append(ts.state.ActivesLawsuits[:idx], ts.state.ActivesLawsuits[idx+1:]...)
	ts.state.LawsuitsDisWithoutMerit = append(ts.state.LawsuitsDisWithoutMerit, a)

	if err := ts.saveLocked(); err != nil {
		return Lawsuit{}, err
	}
	return a, nil
}

// Copy for reading
func (ts *TrialStore) GetActives() []Lawsuit {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	res := make([]Lawsuit, len(ts.state.ActivesLawsuits))
	copy(res, ts.state.ActivesLawsuits)
	return res
}

func (ts *TrialStore) GetDisWithMerit() []Lawsuit {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	res := make([]Lawsuit, len(ts.state.LawsuitsDisWithMerit))
	copy(res, ts.state.LawsuitsDisWithMerit)
	return res
}

func (ts *TrialStore) GetDisWithoutMerit() []Lawsuit {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	res := make([]Lawsuit, len(ts.state.LawsuitsDisWithoutMerit))
	copy(res, ts.state.LawsuitsDisWithoutMerit)
	return res
}

func (ts *TrialStore) CountActives() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.state.ActivesLawsuits)
}

func (ts *TrialStore) GetTrialAddr() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.state.TrialAddr
}

func (ts *TrialStore) GetIDs() (int, int) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.state.DistrictID, ts.state.TrialID
}

func (ts *TrialStore) GetDistrictName() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.state.DistrictName
}

// Update IDs, district's name and trial's address, saving in disc (mirror)
func (ts *TrialStore) UpdateInfo(districtID int, districtName string, trialID int, trialAddr string) error {
	ts.mu.Lock()
	if districtID > 0 {
		ts.state.DistrictID = districtID
	}
	if strings.TrimSpace(districtName) != "" {
		ts.state.DistrictName = strings.TrimSpace(districtName)
	}
	if trialID > 0 {
		ts.state.TrialID = trialID
	}
	if strings.TrimSpace(trialAddr) != "" {
		ts.state.TrialAddr = strings.TrimSpace(trialAddr)
	}
	if ts.state.NextSeq <= 0 {
		ts.state.NextSeq = 1
	}
	err := ts.saveLocked()
	ts.mu.Unlock()
	return err
}

// Update claims of one existent lawsuit (joinder - gathering of lawsuits) 
func (ts *TrialStore) AddClaims(LawsuitID string, newClaims []int) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// helper for claims set 
	addUnique := func(slice []int, val int) []int {
		for _, x := range slice {
			if x == val {
				return slice
			}
		}
		return append(slice, val)
	}

	found := false
	for i := range ts.state.ActivesLawsuits {
		if ts.state.ActivesLawsuits[i].ID == LawsuitID {
			for _, p := range newClaims {
				ts.state.ActivesLawsuits[i].Claims = addUnique(ts.state.ActivesLawsuits[i].Claims, p)
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("lawsuit %s not found between the actives lawsuits for claims' merge", LawsuitID)
	}

	return ts.saveLocked()
}

// Add connection link between two lawsuits (bidirectional, if possible)
func (ts *TrialStore) AddConnection(LawsuitID string, otherID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	addUniqueStr := func(slice []string, val string) []string {
		for _, x := range slice {
			if x == val {
				return slice
			}
		}
		return append(slice, val)
	}

	// found both active lawsuits
	var idx1, idx2 = -1, -1
	for i := range ts.state.ActivesLawsuits {
		if ts.state.ActivesLawsuits[i].ID == LawsuitID {
			idx1 = i
		}
		if ts.state.ActivesLawsuits[i].ID == otherID {
			idx2 = i
		}
	}
	if idx1 == -1 {
		return fmt.Errorf("lawsuit %s not found for connection", LawsuitID)
	}
	if idx2 == -1 {
		// if the other is not here yet, connect only one end
		ts.state.ActivesLawsuits[idx1].Connected = addUniqueStr(ts.state.ActivesLawsuits[idx1].Connected, otherID)
		return ts.saveLocked()
	}

	ts.state.ActivesLawsuits[idx1].Connected = addUniqueStr(ts.state.ActivesLawsuits[idx1].Connected, otherID)
	ts.state.ActivesLawsuits[idx2].Connected = addUniqueStr(ts.state.ActivesLawsuits[idx2].Connected, LawsuitID)

	return ts.saveLocked()
}


// ---------- Search in all lists (for the "Search lawsuit" menu) ----------

type SearchResult struct {
	List     string
	Lawsuit  Lawsuit
}

// District request to the trial start searching lawsuits by simple criteria.
type TrialSearchLawsuitsRequest struct {
	Type  string `json:"type"`  // "search_lawsuit"
	Field string `json:"field"` // "id", "plaintiff", "defendant", "cause", "claim"
	Value string `json:"value"`
}

// Individual search result for lawsuits returned by the trial (flattened, withou field "Lawsuit").
type TrialSearchResult struct {
	List        string `json:"list"`         // "Active", "Dismissed with merit", "Dismissed without merit"
	ID          string `json:"id"`           // Lawsuit ID (ex: "1.1.3")
	Plaintiff   string `json:"plaintiff"`    // Plaintiff's name
	Defendant   string `json:"defendant"`    // Defendant's name
	CauseAction int    `json:"cause_action"` // Cause of action code
	Claims      []int  `json:"claims"`       // Claims' List 
}

// Trial response for the request of lawsuits search
type TrialSearchLawsuitsResponse struct {
	Success      bool                `json:"success"`
	Message      string              `json:"message"`
	DistrictID   int                 `json:"district_id,omitempty"`
	DistrictName string              `json:"district_name,omitempty"`
	TrialID      int                 `json:"trial_id,omitempty"`
	TrialAddr    string              `json:"trial_addr,omitempty"`
	Results      []TrialSearchResult `json:"results,omitempty"`
}

func (ts *TrialStore) SearchLawsuits(field, value string) ([]SearchResult, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	results := []SearchResult{}

	match := func(a Lawsuit) bool {
		switch field {
		case "id":
			return strings.EqualFold(a.ID, value)
		case "plaintiff":
			return strings.Contains(strings.ToLower(a.Plaintiff), strings.ToLower(value))
		case "defendant":
			return strings.Contains(strings.ToLower(a.Defendant), strings.ToLower(value))
		case "cause":
			n, err := strconv.Atoi(value)
			if err != nil {
				return false
			}
			return a.CauseAction == n
		case "claim":
			n, err := strconv.Atoi(value)
			if err != nil {
				return false
			}
			for _, p := range a.Claims {
				if p == n {
					return true
				}
			}
			return false
		default:
			return false
		}
	}

	for _, a := range ts.state.ActivesLawsuits {
		if match(a) {
			results = append(results, SearchResult{List: "Active", Lawsuit: a})
		}
	}
	for _, a := range ts.state.LawsuitsDisWithMerit {
		if match(a) {
			results = append(results, SearchResult{List: "Dismissed with merit", Lawsuit: a})
		}
	}
	for _, a := range ts.state.LawsuitsDisWithoutMerit {
		if match(a) {
			results = append(results, SearchResult{List: "Dismissed without merit", Lawsuit: a})
		}
	}

	return results, nil
}


// ---------- Aux functions for claims comparation ----------

func sameIntSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[int]int, len(a))
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		if m[x] == 0 {
			return false
		}
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func isSubset(sub, sup []int) bool {
	if len(sub) > len(sup) {
		return false
	}
	setSup := make(map[int]bool, len(sup))
	for _, x := range sup {
		setSup[x] = true
	}
	for _, x := range sub {
		if !setSup[x] {
			return false
		}
	}
	return true
}

func hasOverlap(a, b []int) bool {
	set := make(map[int]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if set[x] {
			return true
		}
	}
	return false
}


// ---------- Structures of protocol DISTRICT <-> TRIAL (lawsuit) ----------

// Lawsuit description in the protocol
type ActionQuery struct {
	Plaintiff string `json:"plaintiff"`
	Defendant string `json:"defendant"`
	CauseID   int    `json:"cause_id"`
	Claims    []int  `json:"claims"`
}

// District request for the trial start a searching for lawsuit
type TrialActionQueryRequest struct {
	Type     string      `json:"type"`  // "lawsuit_query"
	Stage    string      `json:"stage"` // "res_judicata", "lis_pendens", "repeated_request", "joinder", "connection"
	Lawsuit  ActionQuery `json:"Lawsuit"`
}

// Trial response about lawsuit
type TrialActionQueryResponse struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`
	Match   string `json:"match"` // "", "res_judicata", "lis_pendens", "repeated_request", "joinder_contained", "joinder_continent", "connection"
	Message string `json:"message"`

	LawsuitID string `json:"Lawsuit_id,omitempty"`

	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`

	ExistentClaims     []int    `json:"existent_claims,omitempty"`
	ConnectedLawsuits  []string `json:"connected_lawsuits,omitempty"`
}

// District request for the trial to create a lawsuit
type TrialCreateActionRequest struct {
	Type    string      `json:"type"` // "lawsuit_create"
	Reason  string      `json:"reason"`
	Lawsuit ActionQuery `json:"Lawsuit"`
	Related string      `json:"related,omitempty"` // ID of related lawsuit
}

type TrialCreateActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	LawsuitID    string `json:"Lawsuit_id,omitempty"`
	DistrictID   int    `json:"district_id,omitempty"`
	DistrictName string `json:"district_name,omitempty"`
	TrialID      int    `json:"trial_id,omitempty"`
	TrialAddr    string `json:"trial_addr,omitempty"`
}

// Request to claims merge (joinder)
type TrialMergeClaimsRequest struct {
	Type      string `json:"type"` // "lawsuit_merge_claims"
	LawsuitID string `json:"Lawsuit_id"`
	NewClaims []int  `json:"new_claims"`
}

type TrialMergeClaimsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}


// ---------- District address persistence ----------

const districtAddrFile = "district_addr.txt"

func loadDistrictAddress(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error while reading district address file (%s): %v", path, err)
		}
		return ""
	}
	addr := strings.TrimSpace(string(b))
	return addr
}

func saveDistrictAddress(path, addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	if err := os.WriteFile(path, []byte(addr+"\n"), 0644); err != nil {
		log.Printf("Error while saving the district address in %s: %v", path, err)
	}
}


// ---------- Protocol with the district (initial handshake) ----------

// Message sent by the TRIAL to the DISTRICT
type DistrictInfoRequest struct {
	Type    string `json:"type"`     // "trial_info"
	TrialID int    `json:"trial_id"` // which trial (1, 2, 3, etc.)
}

// Response that the DISTRICT send to the TRIAL
type DistrictInfoResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	DistrictID   int    `json:"district_id"`
	DistrictName string `json:"district_name"`
	TrialID      int    `json:"trial_id"`
	TrialAddr    string `json:"trial_addr"`
}

// Try to get (from district) DistricID, DistrictName, TrialID and TrialAddr.
// If error, log only; do not stop the initialization.
func getInfoFromDistrict(districtAddr string, trialID int, ts *TrialStore) {
	if trialID <= 0 {
		log.Printf("getInfoFromDistrict: Invalid trialID (%d);  it is not possible to verify the district.", trialID)
		return
	}

	addr, err := net.ResolveUDPAddr("udp", districtAddr)
	if err != nil {
		log.Printf("Error while resolving district address (%s): %v", districtAddr, err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("Error while connecting to the district in %s: %v", districtAddr, err)
		return
	}
	defer conn.Close()

	req := DistrictInfoRequest{
		Type:   "trial_info",
		TrialID: trialID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("Error while decoding JSON for district: %v", err)
		return
	}

	log.Printf("[TRIAL->DISTRICT] %s - sending trial_info (TrialID=%d) to %s",
		time.Now().Format(time.RFC3339), trialID, districtAddr)

	if _, err := conn.Write(data); err != nil {
		log.Printf("Error while sending request to district: %v", err)
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		log.Printf("Error while receiving response from district: %v", err)
		return
	}

	var resp DistrictInfoResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		log.Printf("Error while decoding response from district: %v", err)
		return
	}

	if !resp.Success {
		log.Printf("District responded with error in the trial_info: %s", resp.Message)
		return
	}

	log.Printf("[DISTRICT->TRIAL] %s - trial_info OK: DistrictID=%d, DistrictName=%q, TrialID=%d, TrialAddr=%q",
		time.Now().Format(time.RFC3339),
		resp.DistrictID, resp.DistrictName, resp.TrialID, resp.TrialAddr,
	)

	if err := ts.UpdateInfo(resp.DistrictID, resp.DistrictName, resp.TrialID, resp.TrialAddr); err != nil {
		log.Printf("Error while updating IDs' local mirror of trial: %v", err)
	}
}


// ---------- Search logic for rules 1 to 5 in the trial ----------

func (ts *TrialStore) findIdenticalDwM(list string, q ActionQuery) (Lawsuit, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	match := func(a Lawsuit) bool {
		return strings.EqualFold(a.Plaintiff, q.Plaintiff) &&
			strings.EqualFold(a.Defendant, q.Defendant) &&
			a.CauseAction == q.CauseID &&
			sameIntSet(a.Claims, q.Claims)
	}

	switch list {
	case "dis_with":
		for _, a := range ts.state.LawsuitsDisWithMerit {
			if match(a) {
				return a, true
			}
		}
	case "dis_without":
		for _, a := range ts.state.LawsuitsDisWithoutMerit {
			if match(a) {
				return a, true
			}
		}
	case "actives":
		for _, a := range ts.state.ActivesLawsuits {
			if match(a) {
				return a, true
			}
		}
	}
	return Lawsuit{}, false
}


// Joinder (continence): same parts (plaintiff, defendant), same cause of action,
// but claims with a set relationship (contained/continent).
// Returns:
//   - "joinder_contained": the new lawsuit is CONTAINED in the existent one (does not create a new lawsuit).
//   - "joinder_continent": the new lawsuit is CONTINENT (it is necessay to merge the claims into existent lawsuit).
func (ts *TrialStore) findJoinder(q ActionQuery) (string, Lawsuit, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	for _, a := range ts.state.ActivesLawsuits {
		if !strings.EqualFold(a.Plaintiff, q.Plaintiff) {
			continue
		}
		if !strings.EqualFold(a.Defendant, q.Defendant) {
			continue
		}
		if a.CauseAction != q.CauseID {
			continue
		}

		// equal -> already treated in the previous rules
		if sameIntSet(a.Claims, q.Claims) {
			continue
		}

		if isSubset(q.Claims, a.Claims) {
			return "joinder_contained", a, true
		}
		if isSubset(a.Claims, q.Claims) {
			return "joinder_continent", a, true
		}
	}

	return "", Lawsuit{}, false
}


// Connection: same cause of action and/or common claims (ACTIVES lawsuits),
// BUT **CANNOT** be the case of same parts + cause of action,
// because these cases are reserved as JOINDER
func (ts *TrialStore) findConnection(q ActionQuery) (Lawsuit, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	for _, a := range ts.state.ActivesLawsuits {
		// 1) If have SAME plaintiff, SAME defendant and SAME cause,
		//    this case must be treated in the JOINDER rule,
		//    not in the connection. Jum here.
		if strings.EqualFold(a.Plaintiff, q.Plaintiff) &&
			strings.EqualFold(a.Defendant, q.Defendant) &&
			a.CauseAction == q.CauseID {
			continue
		}

		// 2) Rule for connection properly speaking:
		sameCause := (a.CauseAction == q.CauseID)
		commonClaims := hasOverlap(a.Claims, q.Claims)

		if sameCause || commonClaims {
			return a, true
		}
	}
	return Lawsuit{}, false
}


// ---------- Handlers UDP: lawsuit_query / lawsuit_create / lawsuit_merge_claims ----------

func handleLawsuitQuery(conn net.PacketConn, addr net.Addr, data []byte, ts *TrialStore) {
	var req TrialActionQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error while decoding TrialActionQueryRequest from %s: %v", addr.String(), err)
		return
	}

	districtID, trialID := ts.GetIDs()
	districtName := ts.GetDistrictName()
	trialAddr := ts.GetTrialAddr()

	resp := TrialActionQueryResponse{
		Success:     true,
		Stage:       req.Stage,
		Match:       "none",
		Message:     "no corresponding lawsuit found in this trial",
		DistrictID:   districtID,
		DistrictName: districtName,
		TrialID:      trialID,
		TrialAddr:    trialAddr,
	}

	switch req.Stage {
	case "res_judicata":
		if a, ok := ts.findIdenticalDwM("dis_with", req.Lawsuit); ok {
			resp.Match = "res_judicata"
			resp.Message = "identical lawsuit found in dismissed whith prejudice (merit judgment -> res judicata)."
			resp.LawsuitID = a.ID
		}

	case "lis_pendens":
		if a, ok := ts.findIdenticalDwM("actives", req.Lawsuit); ok {
			resp.Match = "lis_pendens"
			resp.Message = "identical lawsuit found in actives lawsuits (lis pendens)."
			resp.LawsuitID = a.ID
		}

	case "repeated_request":
		if a, ok := ts.findIdenticalDwM("dis_without", req.Lawsuit); ok {
			resp.Match = "repeated_request"
			resp.Message = "identical lawsuit found in dismissed without prejudice (no merit judgment -> repeated request)."
			resp.LawsuitID = a.ID
		}

	case "joinder":
		matchType, a, ok := ts.findJoinder(req.Lawsuit)
		if ok {
			resp.Match = matchType
			switch matchType {
			case "joinder_contained":
				resp.Message = "the new lawsuit is CONTAINED in the already existent lawsuit (smaller claim)."
			case "joinder_continent":
				resp.Message = "the new lawsuit is CONTINENT in relation with already existent lawsuit (bigger claim)."
			}
			resp.LawsuitID = a.ID
			resp.ExistentClaims = append(resp.ExistentClaims, a.Claims...)
		}

	case "connection":
		if a, ok := ts.findConnection(req.Lawsuit); ok {
			resp.Match = "connection"
			resp.Message = "found a connected lawsuit (same cause of action and/or common claims)."
			resp.LawsuitID = a.ID
			if len(a.Connected) > 0 {
				resp.ConnectedLawsuits = append(resp.ConnectedLawsuits, a.Connected...)
			}
		}

	default:
		resp.Success = false
		resp.Message = "unknown stage in the lawsuit_query"
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while decoding TrialActionQueryResponse from %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Error while sending the response lawsuit_query to %s: %v", addr.String(), err)
		return
	}

	log.Printf("[TRIAL] lawsuit_query stage=%s match=%s to %s (Lawsuit_id=%s)",
		resp.Stage, resp.Match, addr.String(), resp.LawsuitID)
}

func handleLawsuitCreate(conn net.PacketConn, addr net.Addr, data []byte, ts *TrialStore) {
	var req TrialCreateActionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error while decoding TrialCreateActionRequest from %s: %v", addr.String(), err)
		return
	}

	districtID, trialID := ts.GetIDs()
	districtName := ts.GetDistrictName()
	trialAddr := ts.GetTrialAddr()

	resp := TrialCreateActionResponse{
		Success:     false,
		Message:     "",
		DistrictID:   districtID,
		DistrictName: districtName,
		TrialID:      trialID,
		TrialAddr:    trialAddr,
	}

	if req.Lawsuit.Plaintiff == "" || req.Lawsuit.Defendant == "" || req.Lawsuit.CauseID == 0 || len(req.Lawsuit.Claims) == 0 {
		resp.Message = "iInsufficient data for the lawsuit in the lawsuit_create"
	} else {
		new_lawsuit, err := ts.CreateLawsuit(
			req.Lawsuit.Plaintiff,
			req.Lawsuit.Defendant,
			req.Lawsuit.CauseID,
			req.Lawsuit.Claims,
			nil,
		)
		if err != nil {
			resp.Message = fmt.Sprintf("error while creating lawsuit: %v", err)
		} else {
			resp.Success = true
			resp.LawsuitID = new_lawsuit.ID
			switch req.Reason {
			case "free":
				resp.Message = "lawsuit created by free distribution"
			case "repeated_request":
				resp.Message = fmt.Sprintf("lawsuit created as REPEATED REQUEST (related to the lawsuit %s)", req.Related)
			case "connection":
				resp.Message = fmt.Sprintf("lawsuit created as CONNECTED to the lawsuit %s", req.Related)
				if req.Related != "" {
					if err := ts.AddConnection(new_lawsuit.ID, req.Related); err != nil {
						log.Printf("Error while registering connection between lawsuits (%s and %s): %v", new_lawsuit.ID, req.Related, err)
					}
				}
			default:
				resp.Message = "lawsuit created"
			}
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while decoding TrialCreateActionResponse for %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Error while sending response lawsuit_create to %s: %v", addr.String(), err)
		return
	}

	log.Printf("[TRIAL] lawsuit_create reason=%s success=%v Lawsuit_id=%s to %s",
		req.Reason, resp.Success, resp.LawsuitID, addr.String())
}

func handleLawsuitMergeClaims(conn net.PacketConn, addr net.Addr, data []byte, ts *TrialStore) {
	var req TrialMergeClaimsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error while decoding TrialMergeClaimsRequest from %s: %v", addr.String(), err)
		return
	}

	resp := TrialMergeClaimsResponse{
		Success: false,
		Message: "",
	}

	if req.LawsuitID == "" || len(req.NewClaims) == 0 {
		resp.Message = "Invalid Lawsuit_id or new_claims in the lawsuit_merge_claims"
	} else {
		if err := ts.AddClaims(req.LawsuitID, req.NewClaims); err != nil {
			resp.Message = fmt.Sprintf("error while merging claims to the lawsuit %s: %v", req.LawsuitID, err)
		} else {
			resp.Success = true
			resp.Message = fmt.Sprintf("claims were merged with success to the lawsuit %s", req.LawsuitID)
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while decoding TrialMergeClaimsResponse to %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Error while sending response lawsuit_merge_claims to %s: %v", addr.String(), err)
		return
	}

	log.Printf("[TRIAL] lawsuit_merge_claims Lawsuit_id=%s success=%v to %s",
		req.LawsuitID, resp.Success, addr.String())
}

// Treats claims of search_Lasuit from district.
func handleSearchLawsuit(conn net.PacketConn, addr net.Addr, data []byte, ts *TrialStore) {
	var req TrialSearchLawsuitsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error while decoding TrialSearchLawsuitsRequest from %s: %v", addr.String(), err)
		return
	}

	districtID, trialID := ts.GetIDs()
	districtName := ts.GetDistrictName()
	trialAddr := ts.GetTrialAddr()

	resp := TrialSearchLawsuitsResponse{
		Success:     true,
		Message:     "",
		DistrictID:   districtID,
		DistrictName: districtName,
		TrialID:      trialID,
		TrialAddr:    trialAddr,
		Results:  []TrialSearchResult{},
	}

	results, err := ts.SearchLawsuits(req.Field, req.Value)
	if err != nil {
		resp.Success = false
		resp.Message = fmt.Sprintf("error while searching for lawsuits: %v", err)
	} else {
		resp.Message = fmt.Sprintf("%d lawsuits found", len(results))

		for _, r := range results {
			a := r.Lawsuit
			resp.Results = append(resp.Results, TrialSearchResult{
				List:        r.List,
				ID:          a.ID,
				Plaintiff:   a.Plaintiff,
				Defendant:   a.Defendant,
				CauseAction: a.CauseAction,
				Claims:      append([]int(nil), a.Claims...),
			})
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while decoding TrialSearchLawsuitsResponse to %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Error while sending response search_lawsuit to %s: %v", addr.String(), err)
		return
	}

	log.Printf("[TRIAL] search_lawsuit field=%s value=%q results=%d to %s",
		req.Field, req.Value, len(resp.Results), addr.String())
}

// Handler to workload_info (workload verification by the district)
type WorkloadInfoRequest struct {
	Type string `json:"type"` // "workload_info"
}

type WorkloadInfoResponse struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	DistrictID      int    `json:"district_id"`
	DistrictName    string `json:"district_name"`
	TrialID         int    `json:"trial_id"`
	TrialAddr       string `json:"trial_addr"`
	ActiveWorkload  int    `json:"active_workload"`
}

func handleWorkloadInfo(conn net.PacketConn, addr net.Addr, ts *TrialStore) {
	districtID, trialID := ts.GetIDs()
	districtName := ts.GetDistrictName()
	trialAddr := ts.GetTrialAddr()
	workload := ts.CountActives()

	resp := WorkloadInfoResponse{
		Success:         true,
		Message:         "Trial's workload successfully returned.",
		DistrictID:      districtID,
		DistrictName:    districtName,
		TrialID:         trialID,
		TrialAddr:       trialAddr,
		ActiveWorkload:  workload,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error while decoding WorkloadInfoResponse for %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Error while sending response workload_info to %s: %v", addr.String(), err)
		return
	}

	log.Printf("[TRIAL] workload_info sent to %s (workload=%d)", addr.String(), workload)
}


// ---------- Generic UDP protocol (fallback) ----------

type GenericResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Dados   interface{} `json:"data,omitempty"`
}

func handlePacket(conn net.PacketConn, addr net.Addr, data []byte, ts *TrialStore) {
	log.Printf("[REQ] %s - package received from %s (%d bytes)",
		time.Now().Format(time.RFC3339), addr.String(), len(data))

	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		log.Printf("Error while decoding message type from %s: %v", addr.String(), err)
		resp := GenericResponse{
			Success: false,
			Message: "error while decoding trial's message",
		}
		b, _ := json.Marshal(resp)
		_, _ = conn.WriteTo(b, addr)
		return
	}

	switch base.Type {
	case "lawsuit_query":
		handleLawsuitQuery(conn, addr, data, ts)
	case "lawsuit_create":
		handleLawsuitCreate(conn, addr, data, ts)
	case "lawsuit_merge_claims":
		handleLawsuitMergeClaims(conn, addr, data, ts)
	case "search_lawsuit":
		handleSearchLawsuit(conn, addr, data, ts)
	case "workload_info":
		handleWorkloadInfo(conn, addr, ts)
	default:
		resp := GenericResponse{
			Success: true,
			Message: "Trial received message, but the type is not recognized by the lawsuits' logic.",
		}
		b, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Error while decoding generic response: %v", err)
			return
		}
		if _, err := conn.WriteTo(b, addr); err != nil {
			log.Printf("Error while sending UDP generic response: %v", err)
			return
		}
		log.Printf("[RESP] %s - generic response sent to %s", time.Now().Format(time.RFC3339), addr.String())
	}
}

// ---------- Clear screen ----------

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
                        // If error, goes to ANSI escape
                        fmt.Print("\033[2J\033[H")
                }
        }
}


// ---------- Interactive Menu ----------

func startMenu(ts *TrialStore, quit chan bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		workload := ts.CountActives()
		districtID, trialID := ts.GetIDs()
		districtName := ts.GetDistrictName()

		// Header with "CIVEL TRIAL number"
		title := "CIVEL TRIAL"
		if trialID > 0 {
			title = fmt.Sprintf("CIVEL TRIAL %d", trialID)
		}

		fmt.Println()
		fmt.Printf("========== %s ==========\n", title)
		if districtName != "" && districtID > 0 {
			fmt.Printf("(District: %s - ID: %d)\n", districtName, districtID)
		} else if districtName != "" {
			fmt.Printf("(District: %s)\n", districtName)
		} else if districtID > 0 {
			fmt.Printf("(District ID: %d)\n", districtID)
		}
		fmt.Printf("Workload (actives lawsuits): %d\n", workload)
		fmt.Println("1 (L) - List lawsuits (actives, dismissed or gathered)")
		fmt.Println("2 (F) - Finish lawsuit")
		fmt.Println("3 (S) - Search lawsuit")
		fmt.Println("4 (Q) - Quit")
		fmt.Println("5 (R) - Refresh (clear screen)")
		fmt.Print("Your option> ")

		line, _ := reader.ReadString('\n')
		opt := strings.TrimSpace(line)

		switch opt {

		case "5","r", "R":
			clearScreen()
			continue

		case "1", "l", "L":
			for {
				clearScreen()
				fmt.Println("\n--- LIST LAWSUITS ---")
				fmt.Println("1 (A) - List active lawsuits")
				fmt.Println("2 (W) - List lawsuits dismissed WITH merit judgment")
				fmt.Println("3 (O) - List lawsuit dismissed WITHOUT merit judgment")
				fmt.Println("4 (G) - List gathered lawsuits (connected)")
				fmt.Println("5 (R) - Return to main menu")
				fmt.Print("Your option> ")

				subLine, _ := reader.ReadString('\n')
				SubOpt := strings.TrimSpace(subLine)

				if SubOpt == "5" || SubOpt == "r" || SubOpt == "R" {
					break
				}

				switch SubOpt {
				case "1", "a", "A":
					actives := ts.GetActives()
					if len(actives) == 0 {
						fmt.Println("(No active lawsuits)")
					} else {
						fmt.Println("\n--- ACTIVE LAWSUITS ---")
						for _, a := range actives {
							fmt.Printf("ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v\n",
								a.ID, a.Plaintiff, a.Defendant, a.CauseAction, a.Claims)
						}
					}
				case "2", "w", "W":
					ext := ts.GetDisWithMerit()
					if len(ext) == 0 {
						fmt.Println("(No lawsuits dismissed with merit judgment)")
					} else {
						fmt.Println("\n--- LAWSUITS DISMISSED WITH MERIT JUDGMENT ---")
						for _, a := range ext {
							fmt.Printf("ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v\n",
								a.ID, a.Plaintiff, a.Defendant, a.CauseAction, a.Claims)
						}
					}
				case "3", "o", "O":
					ext := ts.GetDisWithoutMerit()
					if len(ext) == 0 {
						fmt.Println("(no lawsuits dismissed without merit judgment)")
					} else {
						fmt.Println("\n--- LAWSUITS DISMISSED WITHOUT MERIT JUDGMENT ---")
						for _, a := range ext {
							fmt.Printf("ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v\n",
								a.ID, a.Plaintiff, a.Defendant, a.CauseAction, a.Claims)
						}
					}
				case "4", "g", "G":
					actives := ts.GetActives()
					found := false
					fmt.Println("\n--- GATHERED LAWSUITS (CONNECTED) ---")
					for _, a := range actives {
						if len(a.Connected) > 0 {
							found = true
							fmt.Printf("ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v | Connected: %v\n",
								a.ID, a.Plaintiff, a.Defendant, a.CauseAction, a.Claims, a.Connected)
						}
					}
					if !found {
						fmt.Println("(No gathered/connected lawsuit is registered)")
					}
				default:
					fmt.Println("Invalid option in the list submenu.")
				}

				fmt.Print("\nPress ENTER to return to the list submenu...")
				reader.ReadString('\n')
				clearScreen()
			}

		case "2", "f", "F":
			// Finish lawsuit
			fmt.Print("ID for the lawsuit that will be finished: ")
			idStr, _ := reader.ReadString('\n')
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				fmt.Println("Empty ID. Operation cancelled.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Finish lawsuit WITH merit judgment? (y/n): ")
			respStr, _ := reader.ReadString('\n')
			respStr = strings.TrimSpace(strings.ToLower(respStr))

			switch respStr {
			case "y", "yes", "Y", "Yes" :
				a, err := ts.DismissWithMerit(idStr)
				if err != nil {
					fmt.Println("Error while finishing the lawsuit with merit judgment:", err)
				} else {
					fmt.Printf("Lawsuit %s finished WITH merit judgment.\n", a.ID)
				}
			case "n", "no", "not", "N", "Not":
				a, err := ts.DismissWithoutmerit(idStr)
				if err != nil {
					fmt.Println("Error while finishing lawsuit without merit judgment:", err)
				} else {
					fmt.Printf("Lawsuit %s finished WITHOUT merit judgment.\n", a.ID)
				}
			default:
				fmt.Println("Resposta inválida. Use 's' para sim ou 'n' para não. Operação cancelada.")
				fmt.Println("Invalid response. Use 'y' to yes or 'n' to no. Cancelled operation.")
			}

		case "3", "s", "S":
			// Search lawsuit 
			clearScreen()
			fmt.Println("\nSearch for:")
			fmt.Println("1 (I) - Lawsuit ID")
			fmt.Println("2 (P) - Plaintiff")
			fmt.Println("3 (D) - Defendant")
			fmt.Println("4 (C) - Cause of action (integer)")
			fmt.Println("5 (M) - Claim (integer)")
			fmt.Println("6 (R) - Return to menu")
			fmt.Print("Your option> ")
			fieldStr, _ := reader.ReadString('\n')
			fieldStr = strings.TrimSpace(fieldStr)

			var field string
			switch fieldStr {
			case "1", "i", "I":
				field = "id"
			case "2", "p", "P":
				field = "plaintiff"
			case "3", "d", "D":
				field = "defendant"
			case "4", "c", "C":
				field = "cause"
			case "5", "m", "M":
				field = "claim"
			case "6", "r", "R":
				clearScreen()
				continue
			default:
				fmt.Println("\nInvalid field option.")
				fmt.Print("\nPress ENTER to return ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Search value> ")
			val, _ := reader.ReadString('\n')
			val = strings.TrimSpace(val)
			if val == "" {
				fmt.Println("\nEmptyh search value.")
				fmt.Print("\nPress ENTER to return to menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			results, err := ts.SearchLawsuits(field, val)
			if err != nil {
				fmt.Println("\nError in the search:", err)
			} else if len(results) == 0 {
				fmt.Println("\nNo lawsuit found.")
			} else {
				fmt.Println("\n--- SEARCH RESULTS ---")
				for _, r := range results {
					a := r.Lawsuit
					fmt.Printf("[%s] ID: %s | Plaintiff: %s | Defendant: %s | Cause: %d | Claims: %v\n",
						r.List, a.ID, a.Plaintiff, a.Defendant, a.CauseAction, a.Claims)
				}
			}

		case "4", "q", "Q":
			if err := ts.Save(); err != nil {
				log.Printf("\nError while saving lawsuits during quit: %v", err)
			}
			fmt.Println("\nData saved. Finishing the trial.")
			quit <- true
			return

		default:
			fmt.Println("\nInvalid option.")
		}

		// General pause before returning to menu
		fmt.Print("\nPress ENTER to return to menu...")
		reader.ReadString('\n')
		clearScreen()
	}
}


// ---------- MAIN ----------
func main() {
	helpFlag := flag.Bool("h", false, "Show help")
	infoFlag := flag.Bool("info", false, "Show information about option flags")
	districtAddrFlag := flag.String("district", "", "District's UDP address for this trial")
	trialIDFlag := flag.Int("id", 0, "Numeric ID for the trial (1, 2, 3, ...)")
	logFlag := flag.String("log", "", "Log file (or 'term' to log to terminal; default: trial.log)")
	lawsuitsFile := flag.String("lawsuits", "lawsuits.json", "JSON file  with the states for the trial's lawsuits")
	flag.Parse()

	if *helpFlag {
		fmt.Println("Program used to simulate the functioning of a civel trial,")
		fmt.Println("with lists of actives and dismissed lawsuits (with and without merit judgment)")
		fmt.Println("and responding the distribution requests (res judicata, lis pendens, etc.).")
		fmt.Println("\n Release: ",Release)
		fmt.Println()
		fmt.Println("Usage: trial [-h] [-info] -district <district's UDP address> [-id <id_trial>]")
		fmt.Println("            [-log <file_name|term>] [-lawsuits <json_file>]")
		fmt.Println()
		fmt.Println("The trial's UDP address is get from the district (and mirrored on disc).")
		return
	}

	// Uses -info as the default behavior for -h
	if *infoFlag {
		flag.Usage()
		os.Exit(0)
	}

	// LOG configuration
	if *logFlag == "" {
		logFile, err := os.OpenFile("trial.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error while opening default log file (trial.log):", err)
		} else {
			log.SetOutput(logFile)
		}
	} else if *logFlag == "term" {
		// default output (stderr)
	} else {
		logFile, err := os.OpenFile(*logFlag,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Error while opening log file:", err)
		} else {
			log.SetOutput(logFile)
		}
	}

	// Load the state (local mirror)
	ts := NewTrialStore(*lawsuitsFile)
	if err := ts.Load(); err != nil {
		fmt.Println("Error while loading lawsuits from disc:", err)
	}

	// Resolve district's address: flag -> file
	districtAddr := strings.TrimSpace(*districtAddrFlag)
	if districtAddr == "" {
		districtAddr = loadDistrictAddress(districtAddrFile)
	} else {
		actual := loadDistrictAddress(districtAddrFile)
		if districtAddr != actual {
			saveDistrictAddress(districtAddrFile, districtAddr)
		}
	}

	if districtAddr == "" {
		fmt.Println("Error: it was not possible to determine the district's UDP address (use -district or configure", districtAddrFile, ").")
		return
	}

	// Determine TrialID: flag -> mirror
	trialID := *trialIDFlag
	if trialID <= 0 {
		_, storedTrialID := ts.GetIDs()
		trialID = storedTrialID
	}

	if trialID <= 0 {
		fmt.Println("Error: it is necessary to inform the trial's ID (-id or already have an ID saved on disc).")
		return
	}

	// Update the mirror with given TrialID (without modifying other things)
	_ = ts.UpdateInfo(0, "", trialID, "")

	// Handshake wht the district to get DistrictID, DistrictName, TrialAddr
	getInfoFromDistrict(districtAddr, trialID, ts)

	// Trial's final address: the one that is in the mirror (from the district or from previous execution)
	udpAddr := ts.GetTrialAddr()
	if udpAddr == "" {
		fmt.Println("Error: it was not possible to determine the trial's UDP address from the district or from the local mirror.")
		return
	}

	districtID, finalTrialID := ts.GetIDs()
	districtName := ts.GetDistrictName()
	log.Printf("Initialization for TRIAL: DistrictID=%d, DistrictName=%q, TrialID=%d, TrialAddr=%s, DistrictAddr=%s",
		districtID, districtName, finalTrialID, udpAddr, districtAddr)

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Printf("initialization for TRIAL: DistrictID=%d, DistrictName=%q, TrialID=%d, TrialAddr=%s, DistrictAddr=%s",
		districtID, districtName, finalTrialID, udpAddr, districtAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()

	quit := make(chan bool)
	go startMenu(ts, quit)

	// UDP server
	conn, err := net.ListenPacket("udp", udpAddr)
	if err != nil {
		fmt.Println("Error while opening UDP:", err)
		return
	}
	defer conn.Close()

	if districtName != "" {
		log.Printf("Trial server running on %s (Trial %d, District: %s, ID District=%d) - district on %s\n",
			udpAddr, finalTrialID, districtName, districtID, districtAddr)
	} else {
		log.Printf("Trial server running on %s (DistrictID=%d, TrialID=%d) - district on %s\n",
			udpAddr, districtID, finalTrialID, districtAddr)
	}

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
				log.Printf("Error while reading UDP package: %v", err)
				continue
			}

			data := make([]byte, n)
			copy(data, buf[:n])

			go handlePacket(conn, addr, data, ts)
		}
	}
}
