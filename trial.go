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


Revision History for comarca.go:

   Release   Author   Date           Description
    1.0.0    A/F/R    19/NOV/2025    Initial stable release

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

// Identificação da release
const Release = "1.0.0"


// ---------- Estruturas de dados ----------

// Ação com ID "ID_Comarca.ID_Vara.Sequência"
// Agora com lista de pedidos (Pedidos []int) e possível lista de ações conexas.
// PedidoLegacy serve apenas para ler arquivos antigos (onde havia só um "pedido" int)
// e é limpado antes de salvar de novo.
type Acao struct {
	ID         string   `json:"id"`
	Autor      string   `json:"autor"`
	Reu        string   `json:"reu"`
	CausaPedir int      `json:"causa_pedir"`
	Pedidos    []int    `json:"pedidos,omitempty"`
	Conexas    []string `json:"conexas,omitempty"`

	// Campo legado para migração de arquivos antigos (onde existia apenas um "pedido" int).
	PedidoLegacy int `json:"pedido,omitempty"`
}

// Estado completo da vara (persistido em JSON)
type VaraState struct {
	ComarcaID         int    `json:"comarca_id"`
	ComarcaNome       string `json:"comarca_nome"`
	VaraID            int    `json:"vara_id"`
	VaraAddr          string `json:"vara_addr"`
	NextSeq           int    `json:"next_seq"`
	AcoesAtivas       []Acao `json:"acoes_ativas"`
	AcoesExtComMerito []Acao `json:"acoes_extintas_com_merito"`
	AcoesExtSemMerito []Acao `json:"acoes_extintas_sem_merito"`
}

// Wrapper com mutex + caminho do arquivo
type VaraStore struct {
	mu      sync.RWMutex
	state   VaraState
	arqPath string
}

// Cria um novo store com arquivo (IDs serão preenchidos pelo handshake / espelho)
func NovaVaraStore(arqPath string) *VaraStore {
	return &VaraStore{
		state: VaraState{
			ComarcaID:         0,
			ComarcaNome:       "",
			VaraID:            0,
			VaraAddr:          "",
			NextSeq:           1,
			AcoesAtivas:       []Acao{},
			AcoesExtComMerito: []Acao{},
			AcoesExtSemMerito: []Acao{},
		},
		arqPath: arqPath,
	}
}

// migração das ações com campo legado "pedido" -> "pedidos"
func migrateLegacyPedidos(a *Acao) {
	if len(a.Pedidos) == 0 && a.PedidoLegacy != 0 {
		a.Pedidos = []int{a.PedidoLegacy}
		a.PedidoLegacy = 0
	}
}

func (vs *VaraStore) Load() error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	f, err := os.Open(vs.arqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var st VaraState
	if err := dec.Decode(&st); err != nil {
		return err
	}

	// Migração de pedidos legados
	for i := range st.AcoesAtivas {
		migrateLegacyPedidos(&st.AcoesAtivas[i])
	}
	for i := range st.AcoesExtComMerito {
		migrateLegacyPedidos(&st.AcoesExtComMerito[i])
	}
	for i := range st.AcoesExtSemMerito {
		migrateLegacyPedidos(&st.AcoesExtSemMerito[i])
	}

	if st.NextSeq <= 0 {
		st.NextSeq = 1
	}
	vs.state = st
	return nil
}

func (vs *VaraStore) Save() error {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	tmp := vs.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vs.state); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, vs.arqPath)
}

func (vs *VaraStore) saveLocked() error {
	tmp := vs.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vs.state); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, vs.arqPath)
}

func (vs *VaraStore) nextID() string {
	seq := vs.state.NextSeq
	vs.state.NextSeq++
	return fmt.Sprintf("%d.%d.%d", vs.state.ComarcaID, vs.state.VaraID, seq)
}

// Cria uma nova ação ATIVA (com lista de pedidos e possível lista de conexas)
func (vs *VaraStore) CriarAcao(autor, reu string, causa int, pedidos []int, conexas []string) (Acao, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	id := vs.nextID()
	a := Acao{
		ID:         id,
		Autor:      autor,
		Reu:        reu,
		CausaPedir: causa,
		Pedidos:    append([]int(nil), pedidos...),
		Conexas:    append([]string(nil), conexas...),
	}
	vs.state.AcoesAtivas = append(vs.state.AcoesAtivas, a)

	if err := vs.saveLocked(); err != nil {
		return Acao{}, err
	}
	return a, nil
}

// Extingue ação (ativa -> extinta COM mérito)
func (vs *VaraStore) ExtinguirComMerito(id string) (Acao, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	idx := -1
	var a Acao
	for i, ac := range vs.state.AcoesAtivas {
		if ac.ID == id {
			idx = i
			a = ac
			break
		}
	}
	if idx == -1 {
		return Acao{}, fmt.Errorf("ação %q não encontrada na lista de ativas", id)
	}

	vs.state.AcoesAtivas = append(vs.state.AcoesAtivas[:idx], vs.state.AcoesAtivas[idx+1:]...)
	vs.state.AcoesExtComMerito = append(vs.state.AcoesExtComMerito, a)

	if err := vs.saveLocked(); err != nil {
		return Acao{}, err
	}
	return a, nil
}

// Extingue ação (ativa -> extinta SEM mérito)
func (vs *VaraStore) ExtinguirSemMerito(id string) (Acao, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	idx := -1
	var a Acao
	for i, ac := range vs.state.AcoesAtivas {
		if ac.ID == id {
			idx = i
			a = ac
			break
		}
	}
	if idx == -1 {
		return Acao{}, fmt.Errorf("ação %q não encontrada na lista de ativas", id)
	}

	vs.state.AcoesAtivas = append(vs.state.AcoesAtivas[:idx], vs.state.AcoesAtivas[idx+1:]...)
	vs.state.AcoesExtSemMerito = append(vs.state.AcoesExtSemMerito, a)

	if err := vs.saveLocked(); err != nil {
		return Acao{}, err
	}
	return a, nil
}

// Cópias para leitura
func (vs *VaraStore) GetAtivas() []Acao {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	res := make([]Acao, len(vs.state.AcoesAtivas))
	copy(res, vs.state.AcoesAtivas)
	return res
}

func (vs *VaraStore) GetExtComMerito() []Acao {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	res := make([]Acao, len(vs.state.AcoesExtComMerito))
	copy(res, vs.state.AcoesExtComMerito)
	return res
}

func (vs *VaraStore) GetExtSemMerito() []Acao {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	res := make([]Acao, len(vs.state.AcoesExtSemMerito))
	copy(res, vs.state.AcoesExtSemMerito)
	return res
}

func (vs *VaraStore) CountAtivas() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.state.AcoesAtivas)
}

func (vs *VaraStore) GetVaraAddr() string {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.state.VaraAddr
}

func (vs *VaraStore) GetIDs() (int, int) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.state.ComarcaID, vs.state.VaraID
}

func (vs *VaraStore) GetComarcaNome() string {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.state.ComarcaNome
}

// Atualiza IDs, nome da comarca e endereço da vara, salvando em disco (espelho)
func (vs *VaraStore) UpdateInfo(comarcaID int, comarcaNome string, varaID int, varaAddr string) error {
	vs.mu.Lock()
	if comarcaID > 0 {
		vs.state.ComarcaID = comarcaID
	}
	if strings.TrimSpace(comarcaNome) != "" {
		vs.state.ComarcaNome = strings.TrimSpace(comarcaNome)
	}
	if varaID > 0 {
		vs.state.VaraID = varaID
	}
	if strings.TrimSpace(varaAddr) != "" {
		vs.state.VaraAddr = strings.TrimSpace(varaAddr)
	}
	if vs.state.NextSeq <= 0 {
		vs.state.NextSeq = 1
	}
	err := vs.saveLocked()
	vs.mu.Unlock()
	return err
}

// Atualiza pedidos de uma ação existente (continência - reunião de pedidos)
func (vs *VaraStore) AddPedidos(acaoID string, pedidosNovos []int) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// helper para set de pedidos
	addUnique := func(slice []int, val int) []int {
		for _, x := range slice {
			if x == val {
				return slice
			}
		}
		return append(slice, val)
	}

	encontrada := false
	for i := range vs.state.AcoesAtivas {
		if vs.state.AcoesAtivas[i].ID == acaoID {
			for _, p := range pedidosNovos {
				vs.state.AcoesAtivas[i].Pedidos = addUnique(vs.state.AcoesAtivas[i].Pedidos, p)
			}
			encontrada = true
			break
		}
	}
	if !encontrada {
		return fmt.Errorf("ação %s não encontrada entre as ações ativas para merge de pedidos", acaoID)
	}

	return vs.saveLocked()
}

// Adiciona ligação de conexão entre duas ações (bidirecional, se possível)
func (vs *VaraStore) AddConexao(acaoID string, outraID string) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	addUniqueStr := func(slice []string, val string) []string {
		for _, x := range slice {
			if x == val {
				return slice
			}
		}
		return append(slice, val)
	}

	// encontra ambas ações ativas
	var idx1, idx2 = -1, -1
	for i := range vs.state.AcoesAtivas {
		if vs.state.AcoesAtivas[i].ID == acaoID {
			idx1 = i
		}
		if vs.state.AcoesAtivas[i].ID == outraID {
			idx2 = i
		}
	}
	if idx1 == -1 {
		return fmt.Errorf("ação %s não encontrada para conexão", acaoID)
	}
	if idx2 == -1 {
		// se a outra ainda não está aqui, conecta só uma ponta
		vs.state.AcoesAtivas[idx1].Conexas = addUniqueStr(vs.state.AcoesAtivas[idx1].Conexas, outraID)
		return vs.saveLocked()
	}

	vs.state.AcoesAtivas[idx1].Conexas = addUniqueStr(vs.state.AcoesAtivas[idx1].Conexas, outraID)
	vs.state.AcoesAtivas[idx2].Conexas = addUniqueStr(vs.state.AcoesAtivas[idx2].Conexas, acaoID)

	return vs.saveLocked()
}


// ---------- Busca em todas as listas (para o menu "Buscar ação") ----------

type ResultadoBusca struct {
	Lista string
	Acao  Acao
}

// Pedido da comarca para a vara buscar ações por um critério simples.
type VaraBuscarAcoesRequest struct {
	Type  string `json:"type"`  // "acao_buscar"
	Campo string `json:"campo"` // "id", "autor", "reu", "causa", "pedido"
	Valor string `json:"valor"`
}

// Resultado individual de busca de ações retornado pela vara (achatado, sem campo "acao").
type VaraBuscaResultado struct {
	Lista      string `json:"lista"`       // "Ativa", "Extinta com mérito", "Extinta sem mérito"
	ID         string `json:"id"`          // ID da ação (ex: "1.1.3")
	Autor      string `json:"autor"`       // Nome do autor
	Reu        string `json:"reu"`         // Nome do réu
	CausaPedir int    `json:"causa_pedir"` // Código da causa de pedir
	Pedidos    []int  `json:"pedidos"`     // Lista de pedidos
}

// Resposta da vara ao pedido de busca de ações.
type VaraBuscarAcoesResponse struct {
	Success     bool                 `json:"success"`
	Message     string               `json:"message"`
	ComarcaID   int                  `json:"comarca_id,omitempty"`
	ComarcaNome string               `json:"comarca_nome,omitempty"`
	VaraID      int                  `json:"vara_id,omitempty"`
	VaraAddr    string               `json:"vara_addr,omitempty"`
	Resultados  []VaraBuscaResultado `json:"resultados,omitempty"`
}

func (vs *VaraStore) BuscarAcoes(campo, valor string) ([]ResultadoBusca, error) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	resultados := []ResultadoBusca{}

	match := func(a Acao) bool {
		switch campo {
		case "id":
			return strings.EqualFold(a.ID, valor)
		case "autor":
			return strings.Contains(strings.ToLower(a.Autor), strings.ToLower(valor))
		case "reu":
			return strings.Contains(strings.ToLower(a.Reu), strings.ToLower(valor))
		case "causa":
			n, err := strconv.Atoi(valor)
			if err != nil {
				return false
			}
			return a.CausaPedir == n
		case "pedido":
			n, err := strconv.Atoi(valor)
			if err != nil {
				return false
			}
			for _, p := range a.Pedidos {
				if p == n {
					return true
				}
			}
			return false
		default:
			return false
		}
	}

	for _, a := range vs.state.AcoesAtivas {
		if match(a) {
			resultados = append(resultados, ResultadoBusca{Lista: "Ativa", Acao: a})
		}
	}
	for _, a := range vs.state.AcoesExtComMerito {
		if match(a) {
			resultados = append(resultados, ResultadoBusca{Lista: "Extinta com mérito", Acao: a})
		}
	}
	for _, a := range vs.state.AcoesExtSemMerito {
		if match(a) {
			resultados = append(resultados, ResultadoBusca{Lista: "Extinta sem mérito", Acao: a})
		}
	}

	return resultados, nil
}


// ---------- Funções auxiliares de comparação de pedidos ----------

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


// ---------- Estruturas de protocolo COMARCA <-> VARA (ação) ----------

// Descrição da ação no protocolo
type ActionQuery struct {
	Autor   string `json:"autor"`
	Reu     string `json:"reu"`
	CausaID int    `json:"causa_id"`
	Pedidos []int  `json:"pedidos"`
}

// Pedido da comarca para a vara fazer busca por ação
type VaraActionQueryRequest struct {
	Type  string      `json:"type"`  // "acao_query"
	Stage string      `json:"stage"` // "coisa_julgada", "litispendencia", "pedido_reiterado", "continencia", "conexao"
	Acao  ActionQuery `json:"acao"`
}

// Resposta da vara sobre ação
type VaraActionQueryResponse struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`
	Match   string `json:"match"` // "", "coisa_julgada", "litispendencia", "pedido_reiterado", "continencia_contida", "continencia_continente", "conexao"
	Message string `json:"message"`

	AcaoID string `json:"acao_id,omitempty"`

	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	VaraAddr    string `json:"vara_addr,omitempty"`

	PedidosExistentes []int    `json:"pedidos_existentes,omitempty"`
	AcoesConexas      []string `json:"acoes_conexas,omitempty"`
}

// Pedido da comarca para a vara criar ação
type VaraCreateActionRequest struct {
	Type        string      `json:"type"` // "acao_create"
	Motivo      string      `json:"motivo"`
	Acao        ActionQuery `json:"acao"`
	Relacionada string      `json:"relacionada,omitempty"` // ID da ação relacionada
}

type VaraCreateActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	AcaoID      string `json:"acao_id,omitempty"`
	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	VaraAddr    string `json:"vara_addr,omitempty"`
}

// Pedido para merge de pedidos (continência)
type VaraMergePedidosRequest struct {
	Type         string `json:"type"` // "acao_merge_pedidos"
	AcaoID       string `json:"acao_id"`
	PedidosNovos []int  `json:"pedidos_novos"`
}

type VaraMergePedidosResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}


// ---------- Persistência do endereço da comarca ----------

const comarcaAddrFile = "comarca_addr.txt"

func carregarEnderecoComarca(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Erro ao ler arquivo de endereço da comarca (%s): %v", path, err)
		}
		return ""
	}
	addr := strings.TrimSpace(string(b))
	return addr
}

func salvarEnderecoComarca(path, addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	if err := os.WriteFile(path, []byte(addr+"\n"), 0644); err != nil {
		log.Printf("Erro ao salvar endereço da comarca em %s: %v", path, err)
	}
}


// ---------- Protocolo com a comarca (handshake inicial) ----------

// Mensagem que a VARA envia para a COMARCA
type ComarcaInfoRequest struct {
	Type   string `json:"type"`    // "vara_info"
	VaraID int    `json:"vara_id"` // qual vara (1, 2, 3, etc.)
}

// Resposta que a COMARCA envia para a VARA
type ComarcaInfoResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ComarcaID   int    `json:"comarca_id"`
	ComarcaNome string `json:"comarca_nome"`
	VaraID      int    `json:"vara_id"`
	VaraAddr    string `json:"vara_addr"`
}

// Tenta obter (da comarca) ComarcaID, ComarcaNome, VaraID e VaraAddr.
// Em caso de erro, só loga; não interrompe a inicialização.
func obterInfoDaComarca(comarcaAddr string, varaID int, vs *VaraStore) {
	if varaID <= 0 {
		log.Printf("obterInfoDaComarca: VaraID inválido (%d); não é possível consultar comarca.", varaID)
		return
	}

	addr, err := net.ResolveUDPAddr("udp", comarcaAddr)
	if err != nil {
		log.Printf("Erro ao resolver endereço da comarca (%s): %v", comarcaAddr, err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("Erro ao conectar à comarca em %s: %v", comarcaAddr, err)
		return
	}
	defer conn.Close()

	req := ComarcaInfoRequest{
		Type:   "vara_info",
		VaraID: varaID,
	}

	dados, err := json.Marshal(req)
	if err != nil {
		log.Printf("Erro ao codificar JSON para comarca: %v", err)
		return
	}

	log.Printf("[VARA->COMARCA] %s - enviando vara_info (VaraID=%d) para %s",
		time.Now().Format(time.RFC3339), varaID, comarcaAddr)

	if _, err := conn.Write(dados); err != nil {
		log.Printf("Erro ao enviar requisição para comarca: %v", err)
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		log.Printf("Erro ao receber resposta da comarca: %v", err)
		return
	}

	var resp ComarcaInfoResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		log.Printf("Erro ao decodificar resposta da comarca: %v", err)
		return
	}

	if !resp.Success {
		log.Printf("Comarca respondeu erro no vara_info: %s", resp.Message)
		return
	}

	log.Printf("[COMARCA->VARA] %s - vara_info OK: ComarcaID=%d, ComarcaNome=%q, VaraID=%d, VaraAddr=%q",
		time.Now().Format(time.RFC3339),
		resp.ComarcaID, resp.ComarcaNome, resp.VaraID, resp.VaraAddr,
	)

	if err := vs.UpdateInfo(resp.ComarcaID, resp.ComarcaNome, resp.VaraID, resp.VaraAddr); err != nil {
		log.Printf("Erro ao atualizar espelho local de IDs da vara: %v", err)
	}
}


// ---------- Lógica de busca para as regras 1 a 5 na vara ----------

func (vs *VaraStore) findIdenticaEm(lista string, q ActionQuery) (Acao, bool) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	match := func(a Acao) bool {
		return strings.EqualFold(a.Autor, q.Autor) &&
			strings.EqualFold(a.Reu, q.Reu) &&
			a.CausaPedir == q.CausaID &&
			sameIntSet(a.Pedidos, q.Pedidos)
	}

	switch lista {
	case "ext_com":
		for _, a := range vs.state.AcoesExtComMerito {
			if match(a) {
				return a, true
			}
		}
	case "ext_sem":
		for _, a := range vs.state.AcoesExtSemMerito {
			if match(a) {
				return a, true
			}
		}
	case "ativas":
		for _, a := range vs.state.AcoesAtivas {
			if match(a) {
				return a, true
			}
		}
	}
	return Acao{}, false
}


// Continência: mesmas partes (autor, réu), mesma causa de pedir,
// mas pedidos com relação de conjunto (contido/continente).
// Retorna:
//   - "continencia_contida": nova ação é CONTIDA na existente (não cria nova).
//   - "continencia_continente": nova é CONTINENTE (precisa agregar pedidos à existente).
func (vs *VaraStore) findContinencia(q ActionQuery) (string, Acao, bool) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	for _, a := range vs.state.AcoesAtivas {
		if !strings.EqualFold(a.Autor, q.Autor) {
			continue
		}
		if !strings.EqualFold(a.Reu, q.Reu) {
			continue
		}
		if a.CausaPedir != q.CausaID {
			continue
		}

		// igual -> já seria tratado nas regras anteriores
		if sameIntSet(a.Pedidos, q.Pedidos) {
			continue
		}

		if isSubset(q.Pedidos, a.Pedidos) {
			return "continencia_contida", a, true
		}
		if isSubset(a.Pedidos, q.Pedidos) {
			return "continencia_continente", a, true
		}
	}

	return "", Acao{}, false
}


// Conexão: mesma causa de pedir e/ou pedidos em comum (ações ATIVAS),
// MAS **NÃO** pode ser caso de mesmas partes + mesma causa de pedir,
// porque esses casos são reservados para CONTINÊNCIA.
func (vs *VaraStore) findConexao(q ActionQuery) (Acao, bool) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	for _, a := range vs.state.AcoesAtivas {
		// 1) Se tiver MESMO autor, MESMO réu e MESMA causa,
		//    esse caso deve ser avaliado na regra de CONTINÊNCIA,
		//    não na conexão. Pulamos aqui.
		if strings.EqualFold(a.Autor, q.Autor) &&
			strings.EqualFold(a.Reu, q.Reu) &&
			a.CausaPedir == q.CausaID {
			continue
		}

		// 2) Regra de conexão propriamente dita:
		mesmaCausa := (a.CausaPedir == q.CausaID)
		pedidosComuns := hasOverlap(a.Pedidos, q.Pedidos)

		if mesmaCausa || pedidosComuns {
			return a, true
		}
	}
	return Acao{}, false
}


// ---------- Handlers UDP: acao_query / acao_create / acao_merge_pedidos ----------

func handleAcaoQuery(conn net.PacketConn, addr net.Addr, data []byte, vs *VaraStore) {
	var req VaraActionQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar VaraActionQueryRequest de %s: %v", addr.String(), err)
		return
	}

	comarcaID, varaID := vs.GetIDs()
	comarcaNome := vs.GetComarcaNome()
	varaAddr := vs.GetVaraAddr()

	resp := VaraActionQueryResponse{
		Success:     true,
		Stage:       req.Stage,
		Match:       "nenhuma",
		Message:     "nenhuma ação correspondente encontrada nesta vara",
		ComarcaID:   comarcaID,
		ComarcaNome: comarcaNome,
		VaraID:      varaID,
		VaraAddr:    varaAddr,
	}

	switch req.Stage {
	case "coisa_julgada":
		if a, ok := vs.findIdenticaEm("ext_com", req.Acao); ok {
			resp.Match = "coisa_julgada"
			resp.Message = "ação idêntica encontrada em extintas com resolução de mérito (coisa julgada)."
			resp.AcaoID = a.ID
		}

	case "litispendencia":
		if a, ok := vs.findIdenticaEm("ativas", req.Acao); ok {
			resp.Match = "litispendencia"
			resp.Message = "ação idêntica encontrada em ações ativas (litispendência)."
			resp.AcaoID = a.ID
		}

	case "pedido_reiterado":
		if a, ok := vs.findIdenticaEm("ext_sem", req.Acao); ok {
			resp.Match = "pedido_reiterado"
			resp.Message = "ação idêntica encontrada em extintas sem resolução de mérito (pedido reiterado)."
			resp.AcaoID = a.ID
		}

	case "continencia":
		matchType, a, ok := vs.findContinencia(req.Acao)
		if ok {
			resp.Match = matchType
			switch matchType {
			case "continencia_contida":
				resp.Message = "nova ação é CONTIDA em ação já existente (pedido menor)."
			case "continencia_continente":
				resp.Message = "nova ação é CONTINENTE em relação à ação existente (pedido maior)."
			}
			resp.AcaoID = a.ID
			resp.PedidosExistentes = append(resp.PedidosExistentes, a.Pedidos...)
		}

	case "conexao":
		if a, ok := vs.findConexao(req.Acao); ok {
			resp.Match = "conexao"
			resp.Message = "ação conexa encontrada (mesma causa de pedir e/ou pedido em comum)."
			resp.AcaoID = a.ID
			if len(a.Conexas) > 0 {
				resp.AcoesConexas = append(resp.AcoesConexas, a.Conexas...)
			}
		}

	default:
		resp.Success = false
		resp.Message = "stage desconhecido na acao_query"
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar VaraActionQueryResponse para %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Erro ao enviar resposta acao_query para %s: %v", addr.String(), err)
		return
	}

	log.Printf("[VARA] acao_query stage=%s match=%s para %s (acao_id=%s)",
		resp.Stage, resp.Match, addr.String(), resp.AcaoID)
}

func handleAcaoCreate(conn net.PacketConn, addr net.Addr, data []byte, vs *VaraStore) {
	var req VaraCreateActionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar VaraCreateActionRequest de %s: %v", addr.String(), err)
		return
	}

	comarcaID, varaID := vs.GetIDs()
	comarcaNome := vs.GetComarcaNome()
	varaAddr := vs.GetVaraAddr()

	resp := VaraCreateActionResponse{
		Success:     false,
		Message:     "",
		ComarcaID:   comarcaID,
		ComarcaNome: comarcaNome,
		VaraID:      varaID,
		VaraAddr:    varaAddr,
	}

	if req.Acao.Autor == "" || req.Acao.Reu == "" || req.Acao.CausaID == 0 || len(req.Acao.Pedidos) == 0 {
		resp.Message = "dados insuficientes da ação na acao_create"
	} else {
		nova, err := vs.CriarAcao(
			req.Acao.Autor,
			req.Acao.Reu,
			req.Acao.CausaID,
			req.Acao.Pedidos,
			nil,
		)
		if err != nil {
			resp.Message = fmt.Sprintf("erro ao criar ação: %v", err)
		} else {
			resp.Success = true
			resp.AcaoID = nova.ID
			switch req.Motivo {
			case "livre":
				resp.Message = "ação criada por distribuição livre"
			case "pedido_reiterado":
				resp.Message = fmt.Sprintf("ação criada como PEDIDO REITERADO (relacionada à ação %s)", req.Relacionada)
			case "conexao":
				resp.Message = fmt.Sprintf("ação criada como CONEXA à ação %s", req.Relacionada)
				if req.Relacionada != "" {
					if err := vs.AddConexao(nova.ID, req.Relacionada); err != nil {
						log.Printf("Erro ao registrar conexão entre ações (%s e %s): %v", nova.ID, req.Relacionada, err)
					}
				}
			default:
				resp.Message = "ação criada"
			}
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar VaraCreateActionResponse para %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Erro ao enviar resposta acao_create para %s: %v", addr.String(), err)
		return
	}

	log.Printf("[VARA] acao_create motivo=%s success=%v acao_id=%s para %s",
		req.Motivo, resp.Success, resp.AcaoID, addr.String())
}

func handleAcaoMergePedidos(conn net.PacketConn, addr net.Addr, data []byte, vs *VaraStore) {
	var req VaraMergePedidosRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar VaraMergePedidosRequest de %s: %v", addr.String(), err)
		return
	}

	resp := VaraMergePedidosResponse{
		Success: false,
		Message: "",
	}

	if req.AcaoID == "" || len(req.PedidosNovos) == 0 {
		resp.Message = "acao_id ou pedidos_novos inválidos na acao_merge_pedidos"
	} else {
		if err := vs.AddPedidos(req.AcaoID, req.PedidosNovos); err != nil {
			resp.Message = fmt.Sprintf("erro ao agregar pedidos à ação %s: %v", req.AcaoID, err)
		} else {
			resp.Success = true
			resp.Message = fmt.Sprintf("pedidos agregados com sucesso à ação %s", req.AcaoID)
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar VaraMergePedidosResponse para %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Erro ao enviar resposta acao_merge_pedidos para %s: %v", addr.String(), err)
		return
	}

	log.Printf("[VARA] acao_merge_pedidos acao_id=%s success=%v para %s",
		req.AcaoID, resp.Success, addr.String())
}

// Trata pedidos de acao_buscar vindos da comarca.
func handleAcaoBuscar(conn net.PacketConn, addr net.Addr, data []byte, vs *VaraStore) {
	var req VaraBuscarAcoesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar VaraBuscarAcoesRequest de %s: %v", addr.String(), err)
		return
	}

	comarcaID, varaID := vs.GetIDs()
	comarcaNome := vs.GetComarcaNome()
	varaAddr := vs.GetVaraAddr()

	resp := VaraBuscarAcoesResponse{
		Success:     true,
		Message:     "",
		ComarcaID:   comarcaID,
		ComarcaNome: comarcaNome,
		VaraID:      varaID,
		VaraAddr:    varaAddr,
		Resultados:  []VaraBuscaResultado{},
	}

	resultados, err := vs.BuscarAcoes(req.Campo, req.Valor)
	if err != nil {
		resp.Success = false
		resp.Message = fmt.Sprintf("erro na busca de ações: %v", err)
	} else {
		resp.Message = fmt.Sprintf("%d ações encontradas", len(resultados))

		for _, r := range resultados {
			a := r.Acao
			resp.Resultados = append(resp.Resultados, VaraBuscaResultado{
				Lista:      r.Lista,
				ID:         a.ID,
				Autor:      a.Autor,
				Reu:        a.Reu,
				CausaPedir: a.CausaPedir,
				Pedidos:    append([]int(nil), a.Pedidos...),
			})
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar VaraBuscarAcoesResponse para %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Erro ao enviar resposta acao_buscar para %s: %v", addr.String(), err)
		return
	}

	log.Printf("[VARA] acao_buscar campo=%s valor=%q resultados=%d para %s",
		req.Campo, req.Valor, len(resp.Resultados), addr.String())
}

// Handler de carga_info (consulta de carga de trabalho pela comarca)
type CargaInfoRequest struct {
	Type string `json:"type"` // "carga_info"
}

type CargaInfoResponse struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	ComarcaID       int    `json:"comarca_id"`
	ComarcaNome     string `json:"comarca_nome"`
	VaraID          int    `json:"vara_id"`
	VaraAddr        string `json:"vara_addr"`
	CargaAtiva      int    `json:"carga_ativa"`
}

func handleCargaInfo(conn net.PacketConn, addr net.Addr, vs *VaraStore) {
	comarcaID, varaID := vs.GetIDs()
	comarcaNome := vs.GetComarcaNome()
	varaAddr := vs.GetVaraAddr()
	carga := vs.CountAtivas()

	resp := CargaInfoResponse{
		Success:         true,
		Message:         "Carga de trabalho da vara retornada com sucesso.",
		ComarcaID:       comarcaID,
		ComarcaNome:     comarcaNome,
		VaraID:          varaID,
		VaraAddr:        varaAddr,
		CargaAtiva:      carga,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar CargaInfoResponse para %s: %v", addr.String(), err)
		return
	}
	if _, err := conn.WriteTo(b, addr); err != nil {
		log.Printf("Erro ao enviar resposta carga_info para %s: %v", addr.String(), err)
		return
	}

	log.Printf("[VARA] carga_info enviado para %s (carga=%d)", addr.String(), carga)
}


// ---------- Protocolo UDP genérico (fallback) ----------

type GenericResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Dados   interface{} `json:"dados,omitempty"`
}

func handlePacket(conn net.PacketConn, addr net.Addr, data []byte, vs *VaraStore) {
	log.Printf("[REQ] %s - pacote recebido de %s (%d bytes)",
		time.Now().Format(time.RFC3339), addr.String(), len(data))

	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		log.Printf("Erro ao decodificar tipo de mensagem de %s: %v", addr.String(), err)
		resp := GenericResponse{
			Success: false,
			Message: "erro ao decodificar mensagem da vara",
		}
		b, _ := json.Marshal(resp)
		_, _ = conn.WriteTo(b, addr)
		return
	}

	switch base.Type {
	case "acao_query":
		handleAcaoQuery(conn, addr, data, vs)
	case "acao_create":
		handleAcaoCreate(conn, addr, data, vs)
	case "acao_merge_pedidos":
		handleAcaoMergePedidos(conn, addr, data, vs)
	case "acao_buscar":
		handleAcaoBuscar(conn, addr, data, vs)
	case "carga_info":
		handleCargaInfo(conn, addr, vs)
	default:
		resp := GenericResponse{
			Success: true,
			Message: "Vara recebeu mensagem, mas o tipo não é reconhecido para lógica de ações.",
		}
		b, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Erro ao codificar resposta genérica: %v", err)
			return
		}
		if _, err := conn.WriteTo(b, addr); err != nil {
			log.Printf("Erro ao enviar resposta genérica UDP: %v", err)
			return
		}
		log.Printf("[RESP] %s - resposta genérica enviada para %s", time.Now().Format(time.RFC3339), addr.String())
	}
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


// ---------- Menu interativo ----------

func iniciarMenu(vs *VaraStore, sair chan bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		carga := vs.CountAtivas()
		comarcaID, varaID := vs.GetIDs()
		comarcaNome := vs.GetComarcaNome()

		// Cabeçalho com "nª VARA CÍVEL"
		titulo := "VARA CÍVEL"
		if varaID > 0 {
			titulo = fmt.Sprintf("%dª VARA CÍVEL", varaID)
		}

		fmt.Println()
		fmt.Printf("========== %s ==========\n", titulo)
		if comarcaNome != "" && comarcaID > 0 {
			fmt.Printf("(Comarca: %s - ID: %d)\n", comarcaNome, comarcaID)
		} else if comarcaNome != "" {
			fmt.Printf("(Comarca: %s)\n", comarcaNome)
		} else if comarcaID > 0 {
			fmt.Printf("(Comarca ID: %d)\n", comarcaID)
		}
		fmt.Printf("Carga de trabalho (ações ativas): %d\n", carga)
		fmt.Println("1 (L) - Listar ações (ativas, extintas ou reunidas)")
		fmt.Println("2 (F) - Finalizar ação")
		fmt.Println("3 (B) - Buscar ações")
		fmt.Println("4 (S) - Sair")
		fmt.Println("5 (R) - Refresh (limpar tela)")
		fmt.Print("Sua opção> ")

		linha, _ := reader.ReadString('\n')
		opc := strings.TrimSpace(linha)

		switch opc {

		case "5","r", "R":
			clearScreen()
			continue

		case "1", "l", "L":
			for {
				clearScreen()
				fmt.Println("\n--- LISTAR AÇÕES ---")
				fmt.Println("1 (A) - Listar ações ativas")
				fmt.Println("2 (C) - Listar ações extintas COM resolução de mérito")
				fmt.Println("3 (S) - Listar ações extintas SEM resolução de mérito")
				fmt.Println("4 (R) - Listar ações reunidas (conexas)")
				fmt.Println("5 (V) - Voltar ao menu principal")
				fmt.Print("Sua opção> ")

				subLinha, _ := reader.ReadString('\n')
				subOpc := strings.TrimSpace(subLinha)

				if subOpc == "5" || subOpc == "v" || subOpc == "V" {
					break
				}

				switch subOpc {
				case "1", "a", "A":
					ativas := vs.GetAtivas()
					if len(ativas) == 0 {
						fmt.Println("(Nenhuma ação ativa)")
					} else {
						fmt.Println("\n--- AÇÕES ATIVAS ---")
						for _, a := range ativas {
							fmt.Printf("ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v\n",
								a.ID, a.Autor, a.Reu, a.CausaPedir, a.Pedidos)
						}
					}
				case "2", "c", "C":
					ext := vs.GetExtComMerito()
					if len(ext) == 0 {
						fmt.Println("(Nenhuma ação extinta com resolução de mérito)")
					} else {
						fmt.Println("\n--- AÇÕES EXTINTAS COM RESOLUÇÃO DE MÉRITO ---")
						for _, a := range ext {
							fmt.Printf("ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v\n",
								a.ID, a.Autor, a.Reu, a.CausaPedir, a.Pedidos)
						}
					}
				case "3", "s", "S":
					ext := vs.GetExtSemMerito()
					if len(ext) == 0 {
						fmt.Println("(Nenhuma ação extinta sem resolução de mérito)")
					} else {
						fmt.Println("\n--- AÇÕES EXTINTAS SEM RESOLUÇÃO DE MÉRITO ---")
						for _, a := range ext {
							fmt.Printf("ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v\n",
								a.ID, a.Autor, a.Reu, a.CausaPedir, a.Pedidos)
						}
					}
				case "4", "r", "R":
					ativas := vs.GetAtivas()
					encontrou := false
					fmt.Println("\n--- AÇÕES REUNIDAS (CONEXAS) ---")
					for _, a := range ativas {
						if len(a.Conexas) > 0 {
							encontrou = true
							fmt.Printf("ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v | Conexas: %v\n",
								a.ID, a.Autor, a.Reu, a.CausaPedir, a.Pedidos, a.Conexas)
						}
					}
					if !encontrou {
						fmt.Println("(Nenhuma ação reunida/conexa cadastrada)")
					}
				default:
					fmt.Println("Opção inválida no submenu de listagem.")
				}

				fmt.Print("\nPressione ENTER para voltar ao submenu de listagem...")
				reader.ReadString('\n')
				clearScreen()
			}

		case "2", "f", "F":
			// Finalizar ação
			fmt.Print("Informe o ID da ação a ser finalizada: ")
			idStr, _ := reader.ReadString('\n')
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				fmt.Println("ID vazio. Operação cancelada.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Finalizar COM resolução de mérito? (s/n): ")
			respStr, _ := reader.ReadString('\n')
			respStr = strings.TrimSpace(strings.ToLower(respStr))

			switch respStr {
			case "s", "sim":
				a, err := vs.ExtinguirComMerito(idStr)
				if err != nil {
					fmt.Println("Erro ao finalizar ação com resolução de mérito:", err)
				} else {
					fmt.Printf("Ação %s finalizada COM resolução de mérito.\n", a.ID)
				}
			case "n", "nao", "não":
				a, err := vs.ExtinguirSemMerito(idStr)
				if err != nil {
					fmt.Println("Erro ao finalizar ação sem resolução de mérito:", err)
				} else {
					fmt.Printf("Ação %s finalizada SEM resolução de mérito.\n", a.ID)
				}
			default:
				fmt.Println("Resposta inválida. Use 's' para sim ou 'n' para não. Operação cancelada.")
			}

		case "3", "b", "B":
			// Buscar ação
			clearScreen()
			fmt.Println("\nBuscar por:")
			fmt.Println("1 (I) - ID da ação")
			fmt.Println("2 (A) - Autor")
			fmt.Println("3 (R) - Réu")
			fmt.Println("4 (C) - Causa de pedir (número exato)")
			fmt.Println("5 (P) - Pedido (número exato)")
			fmt.Println("6 (S) - Retorna ao menu")
			fmt.Print("Sua opção> ")
			campoStr, _ := reader.ReadString('\n')
			campoStr = strings.TrimSpace(campoStr)

			var campo string
			switch campoStr {
			case "1", "i", "I":
				campo = "id"
			case "2", "a", "A":
				campo = "autor"
			case "3", "r", "R":
				campo = "reu"
			case "4", "c", "C":
				campo = "causa"
			case "5", "p", "P":
				campo = "pedido"
			case "6", "s", "S":
				clearScreen()
				continue
			default:
				fmt.Println("\nOpção de campo inválida.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Valor para busca> ")
			val, _ := reader.ReadString('\n')
			val = strings.TrimSpace(val)
			if val == "" {
				fmt.Println("\nValor de busca vazio.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			resultados, err := vs.BuscarAcoes(campo, val)
			if err != nil {
				fmt.Println("\nErro na busca:", err)
			} else if len(resultados) == 0 {
				fmt.Println("\nNenhuma ação encontrada.")
			} else {
				fmt.Println("\n--- RESULTADOS DA BUSCA ---")
				for _, r := range resultados {
					a := r.Acao
					fmt.Printf("[%s] ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v\n",
						r.Lista, a.ID, a.Autor, a.Reu, a.CausaPedir, a.Pedidos)
				}
			}

		case "4", "s", "S":
			if err := vs.Save(); err != nil {
				log.Printf("\nErro ao salvar ações ao sair: %v", err)
			}
			fmt.Println("\nDados salvos. Encerrando vara.")
			sair <- true
			return

		default:
			fmt.Println("\nOpção inválida.")
		}

		// Pausa geral antes de voltar ao menu
		fmt.Print("\nPressione ENTER para voltar ao menu...")
		reader.ReadString('\n')
		clearScreen()
	}
}


// ---------- MAIN ----------
func main() {
	helpFlag := flag.Bool("h", false, "Mostrar help")
	comarcaAddrFlag := flag.String("comarca", "", "Endereço UDP da comarca desta vara")
	varaIDFlag := flag.Int("id", 0, "ID numérico desta vara (1, 2, 3, ...)")
	logFlag := flag.String("log", "", "Arquivo de log (ou 'term' para log no terminal; default: vara.log)")
	acoesFile := flag.String("acoes", "acoes.json", "Arquivo JSON com o estado das ações da vara")
	flag.Parse()

	// Configuração de LOG
	if *logFlag == "" {
		logFile, err := os.OpenFile("vara.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Erro ao abrir arquivo de log padrão (vara.log):", err)
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
		fmt.Println("Programa utilizado para simular o funcionamento de uma Vara Cível,")
		fmt.Println("mantendo listas de ações ativas e extintas (com e sem resolução de mérito)")
		fmt.Println("e respondendo a consultas de distribuição (coisa julgada, litispendência, etc.).")
		fmt.Println("Release: ",Release)
		fmt.Println()
		fmt.Println("Usage: vara [-h] [-info] -comarca <endereco UDP comarca> [-id <id_vara>]")
		fmt.Println("            [-log <arquivo|term>] [-acoes <arquivo_json>]")
		fmt.Println()
		fmt.Println("O endereço UDP da vara é obtido da comarca (e espelhado em disco).")
		return
	}

	// Carrega estado (espelho local)
	vs := NovaVaraStore(*acoesFile)
	if err := vs.Load(); err != nil {
		fmt.Println("Erro ao carregar ações do disco:", err)
	}

	// Resolver endereço da comarca: flag -> arquivo
	comarcaAddr := strings.TrimSpace(*comarcaAddrFlag)
	if comarcaAddr == "" {
		comarcaAddr = carregarEnderecoComarca(comarcaAddrFile)
	} else {
		atual := carregarEnderecoComarca(comarcaAddrFile)
		if comarcaAddr != atual {
			salvarEnderecoComarca(comarcaAddrFile, comarcaAddr)
		}
	}

	if comarcaAddr == "" {
		fmt.Println("Erro: não foi possível determinar o endereço UDP da comarca (use -comarca ou configure", comarcaAddrFile, ").")
		return
	}

	// Determinar VaraID: flag -> espelho
	varaID := *varaIDFlag
	if varaID <= 0 {
		_, storedVaraID := vs.GetIDs()
		varaID = storedVaraID
	}

	if varaID <= 0 {
		fmt.Println("Erro: é necessário informar o ID da vara (via -id ou já ter um ID salvo em disco).")
		return
	}

	// Atualiza espelho com VaraID informado (sem mexer em outras coisas)
	_ = vs.UpdateInfo(0, "", varaID, "")

	// Handshake com a comarca para obter ComarcaID, ComarcaNome, VaraAddr
	obterInfoDaComarca(comarcaAddr, varaID, vs)

	// Endereço final da vara: o que está no espelho (pode ter vindo da comarca ou de execução anterior)
	udpAddr := vs.GetVaraAddr()
	if udpAddr == "" {
		fmt.Println("Erro: não foi possível determinar o endereço UDP desta vara a partir da comarca nem do espelho local.")
		return
	}

	comarcaID, finalVaraID := vs.GetIDs()
	comarcaNome := vs.GetComarcaNome()
	log.Printf("Inicializando VARA: ComarcaID=%d, ComarcaNome=%q, VaraID=%d, VaraAddr=%s, ComarcaAddr=%s",
		comarcaID, comarcaNome, finalVaraID, udpAddr, comarcaAddr)

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Printf("Inicializando VARA: ComarcaID=%d, ComarcaNome=%q, VaraID=%d, VaraAddr=%s, ComarcaAddr=%s",
		comarcaID, comarcaNome, finalVaraID, udpAddr, comarcaAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()

	sair := make(chan bool)
	go iniciarMenu(vs, sair)

	// Servidor UDP
	conn, err := net.ListenPacket("udp", udpAddr)
	if err != nil {
		fmt.Println("Erro ao abrir UDP:", err)
		return
	}
	defer conn.Close()

	if comarcaNome != "" {
		log.Printf("Servidor de vara rodando em %s (%dª Vara, Comarca: %s, ID Comarca=%d) - comarca em %s\n",
			udpAddr, finalVaraID, comarcaNome, comarcaID, comarcaAddr)
	} else {
		log.Printf("Servidor de vara rodando em %s (ComarcaID=%d, VaraID=%d) - comarca em %s\n",
			udpAddr, comarcaID, finalVaraID, comarcaAddr)
	}

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

			go handlePacket(conn, addr, data, vs)
		}
	}
}
