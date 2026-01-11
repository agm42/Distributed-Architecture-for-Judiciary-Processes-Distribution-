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

// Identificação da release
const Release = "1.0.0"


// ---------- Estruturas compartilhadas com o tribunal ----------

type Comarca struct {
	ID       int    `json:"id"`
	Nome     string `json:"nome"`
	Endereco string `json:"endereco"`
	Varas    int    `json:"varas"`
}

type Request struct {
	Type       string `json:"type"`            // "list", "create", "remove", "update_varas"
	Nome       string `json:"nome,omitempty"`  // usado em create/remove/update_varas
	Varas      int    `json:"varas,omitempty"` // create / update_varas
	VarasDelta int    `json:"varas_delta,omitempty"`
}

type Response struct {
	Success  bool      `json:"success"`
	Message  string    `json:"message"`
	Comarca  *Comarca  `json:"comarca,omitempty"`
	Comarcas []Comarca `json:"comarcas,omitempty"`
}


// ---------- Estruturas para comunicação COMARCA <-> VARA ----------

type ComarcaInfoRequest struct {
	Type   string `json:"type"`    // "vara_info"
	VaraID int    `json:"vara_id"` // qual vara (1, 2, 3, etc.)
}

type ComarcaInfoResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	VaraAddr    string `json:"vara_addr,omitempty"`
}


// ---------- Consulta de ações / distribuição (COMARCA -> VARA) ----------

// Descrição da ação a ser consultada/criada
type ActionQuery struct {
	Autor   string `json:"autor"`
	Reu     string `json:"reu"`
	CausaID int    `json:"causa_id"`
	Pedidos []int  `json:"pedidos"`
}

// Pedido da comarca para uma vara procurar a ação em suas listas
// "Stage" corresponde às regras: "coisa_julgada", "litispendencia", "pedido_reiterado",
// "continencia", "conexao"
type VaraActionQueryRequest struct {
	Type  string      `json:"type"`  // "acao_query"
	Stage string      `json:"stage"` // ver acima
	Acao  ActionQuery `json:"acao"`
}

// Resposta da vara sobre a ação
// Match pode ser:
//   - "" ou "nenhuma"
//   - "coisa_julgada"
//   - "litispendencia"
//   - "pedido_reiterado"
//   - "continencia_contida"
//   - "continencia_continente"
//   - "conexao"
type VaraActionQueryResponse struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`
	Match   string `json:"match"`
	Message string `json:"message"`

	AcaoID string `json:"acao_id,omitempty"`

	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	VaraAddr    string `json:"vara_addr,omitempty"`

	PedidosExistentes []int    `json:"pedidos_existentes,omitempty"`
	AcoesConexas      []string `json:"acoes_conexas,omitempty"`
}

// Pedido para criar de fato a ação na vara
// Motivo: "livre", "pedido_reiterado", "conexao"
type VaraCreateActionRequest struct {
	Type        string      `json:"type"` // "acao_create"
	Motivo      string      `json:"motivo"`
	Acao        ActionQuery `json:"acao"`
	Relacionada string      `json:"relacionada,omitempty"` // ID da ação relacionada (pedido reiterado, conexão, etc.)
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

// Pedido para atualizar os pedidos de uma ação (continência: reunião)
type VaraMergePedidosRequest struct {
	Type         string `json:"type"` // "acao_merge_pedidos"
	AcaoID       string `json:"acao_id"`
	PedidosNovos []int  `json:"pedidos_novos"`
}

type VaraMergePedidosResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}


// ---------- NOVO: Busca de ações (COMARCA -> VARA) ----------

// Pedido de busca genérico (campo + valor) enviado pela comarca para cada vara.
// Type = "acao_buscar".
type VaraBuscarAcoesRequest struct {
	Type  string `json:"type"`  // "acao_buscar"
	Campo string `json:"campo"` // "id", "autor", "reu", "causa", "pedido"
	Valor string `json:"valor"`
}

// Resultado individual retornado pela vara para cada ação encontrada
type VaraBuscarAcoesResultado struct {
	Lista      string `json:"lista"`       // "Ativa", "Extinta com mérito", "Extinta sem mérito"
	ID         string `json:"id"`          // ID da ação
	Autor      string `json:"autor"`       // Nome do autor
	Reu        string `json:"reu"`         // Nome do réu
	CausaPedir int    `json:"causa_pedir"` // ID da causa de pedir
	Pedidos    []int  `json:"pedidos"`     // Lista de pedidos
}

// Resposta da vara com a lista de ações que satisfazem o critério
type VaraBuscarAcoesResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	VaraAddr    string `json:"vara_addr,omitempty"`

	Resultados []VaraBuscarAcoesResultado `json:"resultados,omitempty"`
}

// Consulta de carga de trabalho (nº de ações ativas) de uma vara
type VaraCargaRequest struct {
	Type string `json:"type"` // "carga_info"
}

type VaraCargaResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ComarcaID   int    `json:"comarca_id,omitempty"`
	ComarcaNome string `json:"comarca_nome,omitempty"`
	VaraID      int    `json:"vara_id,omitempty"`
	CargaAtiva  int    `json:"carga_ativa"`
}


// ---------- Lista local de comarcas (espelho do tribunal) ----------

type ComarcaList struct {
	mu      sync.RWMutex
	Itens   []Comarca
	arqPath string
}

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

func (cl *ComarcaList) SetAll(list []Comarca) error {
	cl.mu.Lock()
	cl.Itens = list
	cl.mu.Unlock()
	return cl.Save()
}

func (cl *ComarcaList) GetAll() []Comarca {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	res := make([]Comarca, len(cl.Itens))
	copy(res, cl.Itens)
	return res
}


// ---------- Lista local de varas da comarca ----------

type Vara struct {
	ID       int    `json:"id"`
	Endereco string `json:"endereco"`
}

type VaraList struct {
	mu      sync.RWMutex
	Itens   []Vara
	arqPath string
}

func NovaVaraList(arqPath string) *VaraList {
	return &VaraList{
		Itens:   make([]Vara, 0),
		arqPath: arqPath,
	}
}

func (vl *VaraList) Load() error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	f, err := os.Open(vl.arqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var itens []Vara
	if err := dec.Decode(&itens); err != nil {
		return err
	}
	vl.Itens = itens
	return nil
}

func (vl *VaraList) Save() error {
	vl.mu.RLock()
	defer vl.mu.RUnlock()

	tmp := vl.arqPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vl.Itens); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, vl.arqPath)
}

// próximo ID simples
func (vl *VaraList) nextID() int {
	max := 0
	for _, v := range vl.Itens {
		if v.ID > max {
			max = v.ID
		}
	}
	return max + 1
}

func (vl *VaraList) Add(endereco string) (Vara, error) {
	vl.mu.Lock()
	v := Vara{
		ID:       vl.nextID(),
		Endereco: endereco,
	}
	vl.Itens = append(vl.Itens, v)
	vl.mu.Unlock()

	if err := vl.Save(); err != nil {
		return Vara{}, err
	}
	return v, nil
}

func (vl *VaraList) RemoveByID(id int) (Vara, error) {
	vl.mu.Lock()
	idx := -1
	var removed Vara
	for i, v := range vl.Itens {
		if v.ID == id {
			idx = i
			removed = v
			break
		}
	}
	if idx == -1 {
		vl.mu.Unlock()
		return Vara{}, fmt.Errorf("vara com ID %d não encontrada", id)
	}
	vl.Itens = append(vl.Itens[:idx], vl.Itens[idx+1:]...)
	vl.mu.Unlock()

	if err := vl.Save(); err != nil {
		return Vara{}, err
	}
	return removed, nil
}

func (vl *VaraList) GetAll() []Vara {
	vl.mu.RLock()
	defer vl.mu.RUnlock()
	res := make([]Vara, len(vl.Itens))
	copy(res, vl.Itens)
	return res
}

func (vl *VaraList) Count() int {
	vl.mu.RLock()
	defer vl.mu.RUnlock()
	return len(vl.Itens)
}

// Novo: localizar vara pelo ID (usado pela resposta ao vara_info)
func (vl *VaraList) FindByID(id int) (Vara, bool) {
	vl.mu.RLock()
	defer vl.mu.RUnlock()
	for _, v := range vl.Itens {
		if v.ID == id {
			return v, true
		}
	}
	return Vara{}, false
}


// ---------- Persistência do NOME e ENDEREÇO da comarca ----------

const nomeComarcaFile = "comarca_nome.txt"
const addrComarcaFile = "comarca_addr.txt"

func carregarNomeComarca(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Erro ao ler arquivo de nome da comarca (%s): %v", path, err)
		}
		return ""
	}
	nome := strings.TrimSpace(string(b))
	return nome
}

func salvarNomeComarca(path, nome string) {
	nome = strings.TrimSpace(nome)
	if nome == "" {
		return
	}
	if err := os.WriteFile(path, []byte(nome+"\n"), 0644); err != nil {
		log.Printf("Erro ao salvar nome da comarca em %s: %v", path, err)
	}
}

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


// ---------- Comunicação com o tribunal ----------

func sendToTribunal(tribunalAddr string, req Request) (Response, error) {
	var resp Response

	addr, err := net.ResolveUDPAddr("udp", tribunalAddr)
	if err != nil {
		return resp, fmt.Errorf("erro ao resolver endereço do tribunal: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return resp, fmt.Errorf("erro ao conectar ao tribunal: %v", err)
	}
	defer conn.Close()

	dados, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("erro ao codificar JSON: %v", err)
	}

	log.Printf("[COMARCA->TRIBUNAL] %s - enviando req type=%q nome=%q varas=%d para %s",
		time.Now().Format(time.RFC3339),
		req.Type, req.Nome, req.Varas,
		tribunalAddr,
	)

	if _, err := conn.Write(dados); err != nil {
		return resp, fmt.Errorf("erro ao enviar UDP: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return resp, fmt.Errorf("erro ao receber resposta do tribunal: %v", err)
	}

	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return resp, fmt.Errorf("erro ao decodificar resposta JSON: %v", err)
	}

	log.Printf("[TRIBUNAL->COMARCA] %s - resposta success=%v msg=%q comarcas=%d",
		time.Now().Format(time.RFC3339),
		resp.Success, resp.Message, len(resp.Comarcas),
	)

	return resp, nil
}

func atualizarComarcasDoTribunal(tribunalAddr string, cl *ComarcaList) error {
	req := Request{Type: "list"}
	resp, err := sendToTribunal(tribunalAddr, req)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("tribunal respondeu com erro: %s", resp.Message)
	}
	if err := cl.SetAll(resp.Comarcas); err != nil {
		return fmt.Errorf("erro ao salvar lista de comarcas local: %v", err)
	}
	return nil
}

func enviarUpdateVaras(tribunalAddr, nomeComarca string, totalVaras int) error {
	req := Request{
		Type:  "update_varas",
		Nome:  nomeComarca,
		Varas: totalVaras,
	}
	_, err := sendToTribunal(tribunalAddr, req)
	return err
}


// ---------- Handler específico para "vara_info" ----------

func handleVaraInfo(conn *net.UDPConn, remote *net.UDPAddr, data []byte, nomeComarca string, cl *ComarcaList, vl *VaraList) {
	var req ComarcaInfoRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar ComarcaInfoRequest: %v", err)
		return
	}

	log.Printf("[VARA->COMARCA] %s - vara_info recebido de %s (VaraID=%d)",
		time.Now().Format(time.RFC3339),
		remote.String(), req.VaraID,
	)

	// Descobrir ID da comarca a partir do espelho local (se existir)
	comarcaID := 0
	comarcas := cl.GetAll()
	for _, c := range comarcas {
		if c.Nome == nomeComarca {
			comarcaID = c.ID
			break
		}
	}

	// Localiza a vara pelo ID
	v, ok := vl.FindByID(req.VaraID)
	if !ok {
		resp := ComarcaInfoResponse{
			Success: false,
			Message: fmt.Sprintf("Vara com ID %d não encontrada nesta comarca.", req.VaraID),
		}
		b, _ := json.Marshal(resp)
		_, _ = conn.WriteToUDP(b, remote)
		log.Printf("[COMARCA->VARA] vara_info falhou para %s (VaraID=%d): não encontrada",
			remote.String(), req.VaraID)
		return
	}

	// Monta resposta
	resp := ComarcaInfoResponse{
		Success:     true,
		Message:     "Informações da vara obtidas com sucesso.",
		ComarcaID:   comarcaID,
		ComarcaNome: nomeComarca,
		VaraID:      v.ID,
		VaraAddr:    v.Endereco,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Erro ao codificar resposta vara_info: %v", err)
		return
	}

	if _, err := conn.WriteToUDP(b, remote); err != nil {
		log.Printf("Erro ao enviar resposta vara_info para %s: %v", remote.String(), err)
		return
	}

	log.Printf("[COMARCA->VARA] vara_info OK para %s (VaraID=%d, Addr=%s, ComarcaID=%d, Nome=%s)",
		remote.String(), v.ID, v.Endereco, comarcaID, nomeComarca)
}


// ---------- Handler para "acao_query" vindo de OUTRA COMARCA ----------

// Esse handler permite que UMA comarca atue como "agregadora" das suas varas
// para outra comarca. A outra comarca envia um VaraActionQueryRequest (acao_query)
// diretamente para o endereço da comarca, e aqui é repassado para TODAS as varas
// locais com consultarVarasLocalStage e é devolvida uma VaraActionQueryResponse.
func handleAcaoQueryComarca(
	conn *net.UDPConn,
	remote *net.UDPAddr,
	data []byte,
	nomeComarca string,
	cl *ComarcaList,
	vl *VaraList,
) {
	var req VaraActionQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Erro ao decodificar VaraActionQueryRequest (de %s): %v", remote.String(), err)
		return
	}

	log.Printf("[COMARCA<-COMARCA] %s - acao_query stage=%s recebido de %s",
		time.Now().Format(time.RFC3339), req.Stage, remote.String())

	// Converte ActionQuery -> NovaAcao para reaproveitar consultarVarasLocalStage
	nova := actionQueryToNovaAcao(req.Acao)

	// Consulta TODAS as varas locais para o stage solicitado
	respLocal, err := consultarVarasLocalStage(vl, req.Stage, nova, 2*time.Second)
	if err != nil {
		log.Printf("Erro ao consultar varas locais (como COMARCA agregadora) stage=%s: %v", req.Stage, err)
	}

	// Se não encontrou nada, devolve "nenhuma"
	if respLocal == nil || !respLocal.Success || respLocal.Match == "" || respLocal.Match == "nenhuma" {
		vazio := VaraActionQueryResponse{
			Success: true,
			Stage:   req.Stage,
			Match:   "nenhuma",
			Message: "Nenhuma ação correspondente encontrada nesta comarca.",
		}
		b, _ := json.Marshal(vazio)
		_, _ = conn.WriteToUDP(b, remote)
		log.Printf("[COMARCA->COMARCA] %s - acao_query stage=%s sem correspondência, devolvendo 'nenhuma' para %s",
			time.Now().Format(time.RFC3339), req.Stage, remote.String())
		return
	}

	// Garante que o nome/ID da comarca estejam preenchidos
	if respLocal.ComarcaNome == "" || respLocal.ComarcaID == 0 {
		comarcas := cl.GetAll()
		for _, c := range comarcas {
			if c.Nome == nomeComarca {
				respLocal.ComarcaID = c.ID
				respLocal.ComarcaNome = c.Nome
				break
			}
		}
	}

	b, err := json.Marshal(respLocal)
	if err != nil {
		log.Printf("Erro ao codificar resposta acao_query (comarca agregadora): %v", err)
		return
	}

	if _, err := conn.WriteToUDP(b, remote); err != nil {
		log.Printf("Erro ao enviar resposta acao_query (comarca agregadora) para %s: %v", remote.String(), err)
		return
	}

	log.Printf("[COMARCA->COMARCA] %s - acao_query stage=%s match=%s msg=%q para %s",
		time.Now().Format(time.RFC3339), respLocal.Stage, respLocal.Match, respLocal.Message, remote.String())
}


// ---------- Servidor UDP da comarca (para varas) ----------

func iniciarServidorVaras(comarcaAddr, nomeComarca string, cl *ComarcaList, vl *VaraList) {
	addr, err := net.ResolveUDPAddr("udp", comarcaAddr)
	if err != nil {
		log.Printf("Erro ao resolver endereço da comarca (varas): %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("Erro ao abrir UDP para varas em %s: %v", comarcaAddr, err)
		return
	}
	defer conn.Close()

	log.Printf("Servidor de VARAS da comarca escutando em %s", comarcaAddr)

	buf := make([]byte, 4096)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("Erro ao ler UDP de vara: %v", err)
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		// Detecta o tipo da mensagem
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			log.Printf("Erro ao decodificar tipo de mensagem da vara (%s): %v", remote.String(), err)
			continue
		}

		switch base.Type {
		case "vara_info":
			handleVaraInfo(conn, remote, data, nomeComarca, cl, vl)

		case "acao_query":
			// pedido vindo de OUTRA COMARCA para que esta comarca consulte
			// TODAS as suas varas para o stage indicado
			handleAcaoQueryComarca(conn, remote, data, nomeComarca, cl, vl)

		default:
			log.Printf("[COMARCA] %s - tipo de mensagem desconhecido %q de %s",
				time.Now().Format(time.RFC3339), base.Type, remote.String())
		}

	}
}


// ---------- Utilitário: limpar tela ----------
func clearScreen() {
	//fmt.Print("\033[2J\033[H")

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


// ---------- Estrutura simples para nova ação ----------
type NovaAcao struct {
	Autor   string
	Reu     string
	CausaID int
	Pedidos []int
}

func novaAcaoToActionQuery(a NovaAcao) ActionQuery {
	return ActionQuery{
		Autor:   a.Autor,
		Reu:     a.Reu,
		CausaID: a.CausaID,
		Pedidos: a.Pedidos,
	}
}

// Converte ActionQuery (usado nas mensagens) de volta para NovaAcao
func actionQueryToNovaAcao(q ActionQuery) NovaAcao {
	return NovaAcao{
		Autor:   q.Autor,
		Reu:     q.Reu,
		CausaID: q.CausaID,
		// faz cópia do slice para evitar aliasing
		Pedidos: append([]int(nil), q.Pedidos...),
	}
}


// ---------- Funções auxiliares de comunicação com VARAS ----------

func consultarVaraStage(varaAddr string, stage string, acao NovaAcao, timeout time.Duration) (*VaraActionQueryResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", varaAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver endereço da vara %s: %v", varaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar na vara %s: %v", varaAddr, err)
	}
	defer conn.Close()

	req := VaraActionQueryRequest{
		Type:  "acao_query",
		Stage: stage,
		Acao:  novaAcaoToActionQuery(acao),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao codificar JSON para vara %s: %v", varaAddr, err)
	}

	log.Printf("[COMARCA->VARA] %s - enviando acao_query stage=%s para %s",
		time.Now().Format(time.RFC3339), stage, varaAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("erro ao enviar acao_query para vara %s: %v", varaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("erro ao receber resposta da vara %s: %v", varaAddr, err)
	}

	var resp VaraActionQueryResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta da vara %s: %v", varaAddr, err)
	}

	log.Printf("[VARA->COMARCA] %s - resposta stage=%s match=%s msg=%q da vara %s",
		time.Now().Format(time.RFC3339), resp.Stage, resp.Match, resp.Message, varaAddr)

	return &resp, nil
}

// percorre TODAS as varas da comarca local, para determinado estágio/regra
// e retorna a primeira resposta positiva (coisa julgada, litispendência etc.)
func consultarVarasLocalStage(vl *VaraList, stage string, acao NovaAcao, timeout time.Duration) (*VaraActionQueryResponse, error) {
	varas := vl.GetAll()
	for _, v := range varas {
		resp, err := consultarVaraStage(v.Endereco, stage, acao, timeout)
		if err != nil {
			log.Printf("Aviso: falha ao consultar vara %s no stage %s: %v", v.Endereco, stage, err)
			continue
		}
		if resp != nil && resp.Success && resp.Match != "" && resp.Match != "nenhuma" {
			// Se a própria vara não preencher ComarcaNome/ComarcaID,
			// pelo menos garantimos o endereço.
			if resp.VaraAddr == "" {
				resp.VaraAddr = v.Endereco
			}
			return resp, nil
		}
	}
	return nil, nil
}

// Consulta UM endereço de COMARCA (não de vara) para um determinado stage.
// A outra comarca tratará essa mensagem como 'acao_query' agregando TODAS
// as suas varas (via handleAcaoQueryComarca).
func consultarComarcaStage(comarcaAddr string, stage string, acao NovaAcao, timeout time.Duration) (*VaraActionQueryResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", comarcaAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver endereço da comarca %s: %v", comarcaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar na comarca %s: %v", comarcaAddr, err)
	}
	defer conn.Close()

	req := VaraActionQueryRequest{
		Type:  "acao_query",
		Stage: stage,
		Acao:  novaAcaoToActionQuery(acao),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao codificar JSON para comarca %s: %v", comarcaAddr, err)
	}

	log.Printf("[COMARCA->COMARCA] %s - enviando acao_query stage=%s para %s",
		time.Now().Format(time.RFC3339), stage, comarcaAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("erro ao enviar acao_query para comarca %s: %v", comarcaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("erro ao receber resposta da comarca %s: %v", comarcaAddr, err)
	}

	var resp VaraActionQueryResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta da comarca %s: %v", comarcaAddr, err)
	}

	log.Printf("[COMARCA<-COMARCA] %s - resposta stage=%s match=%s msg=%q da comarca %s",
		time.Now().Format(time.RFC3339), resp.Stage, resp.Match, resp.Message, comarcaAddr)

	return &resp, nil
}

// Percorre TODAS as OUTRAS comarcas (diferentes da comarca local) para um
// determinado stage. Retorna a primeira resposta positiva (match != "" / "nenhuma").
func consultarOutrasComarcasStage(
	nomeComarcaLocal string,
	cl *ComarcaList,
	stage string,
	acao NovaAcao,
	timeout time.Duration,
) (*VaraActionQueryResponse, error) {
	comarcas := cl.GetAll()
	for _, c := range comarcas {
		if strings.EqualFold(c.Nome, nomeComarcaLocal) {
			// pula a própria comarca
			continue
		}
		comarcaAddr := strings.TrimSpace(c.Endereco)
		if comarcaAddr == "" {
			continue
		}

		resp, err := consultarComarcaStage(comarcaAddr, stage, acao, timeout)
		if err != nil {
			log.Printf("Aviso: falha ao consultar comarca %s (%s) no stage %s: %v",
				c.Nome, comarcaAddr, stage, err)
			continue
		}
		if resp != nil && resp.Success && resp.Match != "" && resp.Match != "nenhuma" {
			// Garante info da comarca, se veio vazia
			if resp.ComarcaID == 0 {
				resp.ComarcaID = c.ID
			}
			if resp.ComarcaNome == "" {
				resp.ComarcaNome = c.Nome
			}
			return resp, nil
		}
	}
	return nil, nil
}

// Envia pedido de criação de ação para uma vara específica
func criarAcaoNaVaraAddr(varaAddr, motivo, relacionada string, acao NovaAcao, timeout time.Duration) (*VaraCreateActionResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", varaAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver endereço da vara %s: %v", varaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar na vara %s: %v", varaAddr, err)
	}
	defer conn.Close()

	req := VaraCreateActionRequest{
		Type:        "acao_create",
		Motivo:      motivo,
		Acao:        novaAcaoToActionQuery(acao),
		Relacionada: relacionada,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao codificar JSON (acao_create) para vara %s: %v", varaAddr, err)
	}

	log.Printf("[COMARCA->VARA] %s - enviando acao_create motivo=%s para %s (relacionada=%s)",
		time.Now().Format(time.RFC3339), motivo, varaAddr, relacionada)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("erro ao enviar acao_create para vara %s: %v", varaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("erro ao receber resposta de acao_create da vara %s: %v", varaAddr, err)
	}

	var resp VaraCreateActionResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta acao_create da vara %s: %v", varaAddr, err)
	}

	log.Printf("[VARA->COMARCA] %s - resposta acao_create success=%v acao_id=%s msg=%q (vara=%s)",
		time.Now().Format(time.RFC3339), resp.Success, resp.AcaoID, resp.Message, varaAddr)

	return &resp, nil
}

// Envia pedido para MESCLAR pedidos em ação já existente (continência)
func enviarMergePedidosParaVaraAddr(varaAddr, acaoID string, pedidosNovos []int, timeout time.Duration) (*VaraMergePedidosResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", varaAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver endereço da vara %s: %v", varaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar na vara %s: %v", varaAddr, err)
	}
	defer conn.Close()

	req := VaraMergePedidosRequest{
		Type:         "acao_merge_pedidos",
		AcaoID:       acaoID,
		PedidosNovos: pedidosNovos,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao codificar JSON (acao_merge_pedidos) para vara %s: %v", varaAddr, err)
	}

	log.Printf("[COMARCA->VARA] %s - enviando acao_merge_pedidos acao_id=%s para %s",
		time.Now().Format(time.RFC3339), acaoID, varaAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("erro ao enviar acao_merge_pedidos para vara %s: %v", varaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("erro ao receber resposta de acao_merge_pedidos da vara %s: %v", varaAddr, err)
	}

	var resp VaraMergePedidosResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta acao_merge_pedidos da vara %s: %v", varaAddr, err)
	}

	log.Printf("[VARA->COMARCA] %s - resposta acao_merge_pedidos success=%v msg=%q (vara=%s)",
		time.Now().Format(time.RFC3339), resp.Success, resp.Message, varaAddr)

	return &resp, nil
}

// ---------- NOVO: Função para enviar pedido de busca para uma vara ----------
func buscarAcoesNaVara(varaAddr, campo, valor string, timeout time.Duration) (*VaraBuscarAcoesResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", varaAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver endereço da vara %s: %v", varaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar na vara %s: %v", varaAddr, err)
	}
	defer conn.Close()

	req := VaraBuscarAcoesRequest{
		Type:  "acao_buscar",
		Campo: campo,
		Valor: valor,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao codificar JSON (acao_buscar) para vara %s: %v", varaAddr, err)
	}

	log.Printf("[COMARCA->VARA] %s - enviando acao_buscar campo=%s valor=%q para %s",
		time.Now().Format(time.RFC3339), campo, valor, varaAddr)

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("erro ao enviar acao_buscar para vara %s: %v", varaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65535)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("erro ao receber resposta de acao_buscar da vara %s: %v", varaAddr, err)
	}

	var resp VaraBuscarAcoesResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta acao_buscar da vara %s: %v", varaAddr, err)
	}

	log.Printf("[VARA->COMARCA] %s - resposta acao_buscar success=%v resultados=%d msg=%q (vara=%s)",
		time.Now().Format(time.RFC3339), resp.Success, len(resp.Resultados), resp.Message, varaAddr)

	return &resp, nil
}

// Consulta a carga de trabalho (ações ativas) de uma vara específica
func consultarCargaVara(varaAddr string, timeout time.Duration) (int, error) {
	addr, err := net.ResolveUDPAddr("udp", varaAddr)
	if err != nil {
		return 0, fmt.Errorf("erro ao resolver endereço da vara %s: %v", varaAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return 0, fmt.Errorf("erro ao conectar na vara %s: %v", varaAddr, err)
	}
	defer conn.Close()

	req := VaraCargaRequest{Type: "carga_info"}
	data, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("erro ao codificar JSON (carga_info) para vara %s: %v", varaAddr, err)
	}

	log.Printf("[COMARCA->VARA] %s - enviando carga_info para %s",
		time.Now().Format(time.RFC3339), varaAddr)

	if _, err := conn.Write(data); err != nil {
		return 0, fmt.Errorf("erro ao enviar carga_info para vara %s: %v", varaAddr, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return 0, fmt.Errorf("erro ao receber resposta de carga da vara %s: %v", varaAddr, err)
	}

	var resp VaraCargaResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return 0, fmt.Errorf("erro ao decodificar resposta de carga da vara %s: %v", varaAddr, err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("vara %s respondeu falha na consulta de carga: %s", varaAddr, resp.Message)
	}

	return resp.CargaAtiva, nil
}


// ---------- Distribuição LIVRE (regra 6) ----------

func distribuirAcaoLivre(nomeComarca string, vl *VaraList, acao NovaAcao, timeout time.Duration) (string, error) {
	varas := vl.GetAll()
	if len(varas) == 0 {
		return "", fmt.Errorf("não há varas cadastradas nesta comarca")
	}

	// Escolher a vara com MENOR carga de trabalho (menor número de ações ativas)
	var (
		melhorVara  Vara
		melhorCarga int
		achou       bool
	)

	for _, v := range varas {
		carga, err := consultarCargaVara(v.Endereco, timeout)
		if err != nil {
			log.Printf("Aviso: falha ao obter carga da vara %s: %v", v.Endereco, err)
			continue
		}
		if !achou || carga < melhorCarga {
			achou = true
			melhorCarga = carga
			melhorVara = v
		}
	}

	// Se não foi possível obter a carga de nenhuma vara, cai no fallback aleatório
	if !achou {
		rand.Seed(time.Now().UnixNano())
		melhorVara = varas[rand.Intn(len(varas))]
		log.Printf("Distribuição livre: nenhuma carga obtida; escolhendo vara aleatoriamente: %s", melhorVara.Endereco)
	} else {
		log.Printf("Distribuição livre: escolhendo vara %s com carga de trabalho %d", melhorVara.Endereco, melhorCarga)
	}

	createResp, err := criarAcaoNaVaraAddr(melhorVara.Endereco, "livre", "", acao, timeout)
	if err != nil {
		return "", fmt.Errorf("erro ao criar ação por distribuição livre na vara %s: %v", melhorVara.Endereco, err)
	}
	if !createResp.Success {
		return "", fmt.Errorf("vara recusou criação de ação por distribuição livre: %s", createResp.Message)
	}

	acaoID := createResp.AcaoID
	if acaoID == "" {
		acaoID = "(ID não retornado pela vara)"
	}

	msg := fmt.Sprintf(
		"Distribuição LIVRE realizada.\n\nComarca: %s\nVara escolhida: ID %d (endereço %s)\nIdentificação da ação criada: %s\n\nAutor: %s\nRéu: %s\nCausa de pedir (ID): %d\nPedidos (IDs): %v\n",
		strings.ToUpper(nomeComarca),
		createResp.VaraID, melhorVara.Endereco,
		acaoID,
		acao.Autor, acao.Reu, acao.CausaID, acao.Pedidos,
	)

	if achou {
		msg += fmt.Sprintf("\nCritério: vara com menor carga de trabalho (ações ativas = %d) na comarca.\n", melhorCarga)
	} else {
		msg += "\nCritério: não foi possível obter a carga das varas; usada escolha aleatória.\n"
	}

	return msg, nil
}


// ---------- Parser de pedidos (IDs separados por vírgula) ----------

func parsePedidosInput(input string) ([]int, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("nenhum pedido informado")
	}
	partes := strings.Split(s, ",")
	var pedidos []int
	for _, p := range partes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("pedido inválido: %q (esperado número inteiro)", p)
		}
		pedidos = append(pedidos, id)
	}
	if len(pedidos) == 0 {
		return nil, fmt.Errorf("nenhum pedido válido informado")
	}
	return pedidos, nil
}


// ---------- Menu interativo ----------

func main() {
	// Flags
	helpFlag := flag.Bool("h", false, "Mostrar help")
	nomeFlag := flag.String("nome", "", "Nome da comarca (se vazio, usa o nome salvo em arquivo)")
	tribunalAddr := flag.String("tribunal", "127.0.0.1:9000", "Endereço UDP do tribunal")
	addrFlag := flag.String("addr", "", "Endereço UDP desta comarca (para varas). Se vazio, usa arquivo ou busca no tribunal.")
	comarcasFile := flag.String("comarcas", "comarcas_local.json", "Arquivo local de comarcas")
	varasFile := flag.String("varas", "varas.json", "Arquivo local de varas")
	logFlag := flag.String("log", "", "Arquivo de log (ou 'term' para log no terminal; default: comarca.log)")
	flag.Parse()

	// Configuração de LOG
	if *logFlag == "" {
		logFile, err := os.OpenFile("comarca.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Erro ao abrir arquivo de log padrão (comarca.log):", err)
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
		fmt.Println("Usage: comarca [-h] [-info] [-addr <endereco UDP>] [-tribunal <endereco UDP>] [-nome <nome da comarca>] [-log <arquivo|term>]")
		return
	}


	// 1) Resolver NOME da comarca
	nomeFromFile := carregarNomeComarca(nomeComarcaFile)
	nomeComarca := strings.TrimSpace(*nomeFlag)

	if nomeComarca == "" {
		if nomeFromFile == "" {
			log.Println("Erro: nome da comarca não foi informado via -nome nem encontrado em arquivo.")
			os.Exit(1)
		}
		nomeComarca = nomeFromFile
	}

	if nomeComarca != nomeFromFile {
		salvarNomeComarca(nomeComarcaFile, nomeComarca)
	}

	// Lista local de comarcas
	cl := NovaComarcaList(*comarcasFile)
	if err := cl.Load(); err != nil {
		log.Printf("Erro ao carregar comarcas locais: %v", err)
	}

	// 2) Resolver ENDEREÇO da comarca
	comarcaAddr := strings.TrimSpace(*addrFlag)
	if comarcaAddr == "" {
		addrFromFile := carregarEnderecoComarca(addrComarcaFile)
		if addrFromFile != "" {
			comarcaAddr = addrFromFile
		} else {
			log.Printf("Endereço da comarca não informado nem em arquivo. Tentando obter do tribunal para a comarca %q...", nomeComarca)
			if err := atualizarComarcasDoTribunal(*tribunalAddr, cl); err != nil {
				log.Printf("Erro ao tentar obter lista de comarcas do tribunal: %v", err)
			} else {
				comarcas := cl.GetAll()
				for _, c := range comarcas {
					if c.Nome == nomeComarca {
						comarcaAddr = strings.TrimSpace(c.Endereco)
						if comarcaAddr != "" {
							break
						}
					}
				}
			}

			if comarcaAddr == "" {
				log.Println("Erro: não foi possível determinar o endereço UDP da comarca.")
				log.Println("Informe via flag -addr ou configure o arquivo", addrComarcaFile, "ou cadastre a comarca no tribunal com endereço.")
				os.Exit(1)
			}
		}
	}

	addrFromFile := carregarEnderecoComarca(addrComarcaFile)
	if comarcaAddr != addrFromFile {
		salvarEnderecoComarca(addrComarcaFile, comarcaAddr)
	}

	log.Printf("Iniciando COMARCA %q. Tribunal em %s. Comarca escutando varas em %s.",
		nomeComarca, *tribunalAddr, comarcaAddr)

	// Atualizar comarcas do tribunal (melhor effort)
	if err := atualizarComarcasDoTribunal(*tribunalAddr, cl); err != nil {
		log.Printf("Não foi possível atualizar comarcas a partir do tribunal: %v", err)
		log.Printf("Usando lista local (se existir).")
	}

	// Lista local de varas
	vl := NovaVaraList(*varasFile)
	if err := vl.Load(); err != nil {
		log.Printf("Erro ao carregar varas locais: %v", err)
	}

	clearScreen()
	time.Sleep(100 * time.Millisecond)
	clearScreen()
	fmt.Printf("COMARCA %q. Tribunal em %s. Comarca escutando varas em %s.",
		nomeComarca, *tribunalAddr, comarcaAddr)
	time.Sleep(2000 * time.Millisecond)
	clearScreen()


	// Servidor UDP para varas (agora com acesso à lista de comarcas/varas e nome da comarca)
	go iniciarServidorVaras(comarcaAddr, nomeComarca, cl, vl)


	// Menu interativo
	reader := bufio.NewReader(os.Stdin)
	const udpTimeout = 2 * time.Second

	for {
		fmt.Printf("\n========== COMARCA - %s ==========\n", strings.ToUpper(nomeComarca))
		fmt.Println("1 (E) - Entrar com ação")
		fmt.Println("2 (B) - Buscar ações")
		fmt.Println("3 (C) - Listar as comarcas")
		fmt.Println("4 (V) - Listar as varas")
		fmt.Println("5 (A) - Adicionar vara")
		fmt.Println("6 (D) - Remover vara")
		fmt.Println("7 (S) - Sair")
		fmt.Println("8 (R) - Refresh (limpar tela)")
		fmt.Print("Sua opção> ")

		linha, _ := reader.ReadString('\n')
		opc := strings.TrimSpace(linha)

		switch opc {

		case "r", "R":
			clearScreen()
			continue

		case "1", "E", "e":
			// 1) Tentar atualizar lista de comarcas no tribunal
			fmt.Println("\nAtualizando lista de comarcas no tribunal...")
			if err := atualizarComarcasDoTribunal(*tribunalAddr, cl); err != nil {
				fmt.Println("Aviso: não foi possível contactar o tribunal. Usando lista local.")
				log.Printf("Falha ao atualizar comarcas do tribunal antes de entrar com ação: %v", err)
			} else {
				fmt.Println("Lista de comarcas atualizada a partir do tribunal.")
			}

			// 2) Perguntar dados da nova ação
			fmt.Print("\nAutor: ")
			autor, _ := reader.ReadString('\n')
			autor = strings.TrimSpace(autor)

			fmt.Print("Réu: ")
			reu, _ := reader.ReadString('\n')
			reu = strings.TrimSpace(reu)

			fmt.Print("Causa de pedir (ID numérico): ")
			causaStr, _ := reader.ReadString('\n')
			causaStr = strings.TrimSpace(causaStr)
			causaID, err := strconv.Atoi(causaStr)
			if err != nil || causaID <= 0 {
				fmt.Println("Causa de pedir inválida (deve ser número inteiro).")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Pedidos (IDs numéricos separados por vírgula; ex.: 10 ou 10,20,30): ")
			pedStr, _ := reader.ReadString('\n')
			pedStr = strings.TrimSpace(pedStr)
			pedidos, err := parsePedidosInput(pedStr)
			if err != nil {
				fmt.Println("Erro:", err)
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			nova := NovaAcao{
				Autor:   autor,
				Reu:     reu,
				CausaID: causaID,
				Pedidos: pedidos,
			}

			fmt.Println("\nIniciando verificação de distribuição da ação...")
			fmt.Println("1) Coisa julgada")
			// 1) COISA JULGADA
			respCJ, err := consultarVarasLocalStage(vl, "coisa_julgada", nova, udpTimeout)
			if err == nil && respCJ != nil && respCJ.Match == "coisa_julgada" {
				fmt.Println("\n*** COISA JULGADA ***")
				fmt.Println("Foi encontrada ação idêntica (mesmo autor, réu, causa de pedir e pedidos) já extinta COM resolução de mérito.")
				fmt.Printf("Comarca: %s\n", respCJ.ComarcaNome)
				fmt.Printf("Vara: ID %d (%s)\n", respCJ.VaraID, respCJ.VaraAddr)
				fmt.Printf("Identificação da ação: %s\n", respCJ.AcaoID)
				fmt.Println("Não é possível ingressar com nova ação idêntica, pois há trânsito em julgado.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			// Se não achou nada localmente, consulta as OUTRAS comarcas
			if respCJ == nil || !respCJ.Success || respCJ.Match == "" || respCJ.Match == "nenhuma" {
				respCJ, err = consultarOutrasComarcasStage(nomeComarca, cl, "coisa_julgada", nova, udpTimeout)
				if err != nil {
					fmt.Println("Aviso: erro ao consultar outras comarcas para COISA JULGADA:", err)
				}
			}

			if respCJ != nil && respCJ.Success && respCJ.Match == "coisa_julgada" {
				fmt.Println("\n*** COISA JULGADA ***")
				fmt.Println("Foi encontrada ação idêntica (mesmo autor, réu, causa de pedir e pedidos) já extinta COM resolução de mérito.")
				fmt.Printf("Comarca: %s (ID %d)\n", respCJ.ComarcaNome, respCJ.ComarcaID)
				fmt.Printf("Vara: ID %d (%s)\n", respCJ.VaraID, respCJ.VaraAddr)
				fmt.Printf("Identificação da ação: %s\n", respCJ.AcaoID)
				fmt.Println("Não é possível ingressar com nova ação idêntica, pois há trânsito em julgado.")

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				bufio.NewReader(os.Stdin).ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Aviso: falha ao verificar coisa julgada nas varas locais:", err)
			}

			fmt.Println("2) Litispendência")
			// 2) LITISPENDÊNCIA
			respLit, err := consultarVarasLocalStage(vl, "litispendencia", nova, udpTimeout)

			// Se não achou nada localmente, consulta as OUTRAS comarcas
			if respLit == nil || !respLit.Success || respLit.Match == "" || respLit.Match == "nenhuma" {
				respLit, err = consultarOutrasComarcasStage(nomeComarca, cl, "litispendencia", nova, udpTimeout)
				if err != nil {
					fmt.Println("Aviso: erro ao consultar outras comarcas para LITISPENDÊNCIA:", err)
				}
			}

			if respLit != nil && respLit.Success && respLit.Match == "litispendencia" {
				fmt.Println("\n*** LITISPENDÊNCIA ***")
				fmt.Println("Foi encontrada ação idêntica (mesmo autor, réu, causa de pedir e pedidos) na lista de ações ATIVAS.")
				fmt.Printf("Comarca: %s\n", respLit.ComarcaNome)
				fmt.Printf("Vara: ID %d (%s)\n", respLit.VaraID, respLit.VaraAddr)
				fmt.Printf("Identificação da ação ativa: %s\n", respLit.AcaoID)
				fmt.Println("Não será criada nova ação, pois se trata de litispendência.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Aviso: falha ao verificar litispendência nas varas locais:", err)
			}

			fmt.Println("3) Pedido reiterado (extinta SEM resolução de mérito)")
			// 3) PEDIDO REITERADO
			respPR, err := consultarVarasLocalStage(vl, "pedido_reiterado", nova, udpTimeout)

			// Se não encontrou nada localmente, consultar OUTRAS comarcas
			if respPR == nil || !respPR.Success || respPR.Match == "" || respPR.Match == "nenhuma" {
				respPR, err = consultarOutrasComarcasStage(nomeComarca, cl, "pedido_reiterado", nova, udpTimeout)
				if err != nil {
					fmt.Println("Aviso: erro ao consultar outras comarcas para PEDIDO REITERADO:", err)
				}
			}

			if respPR != nil && respPR.Success && respPR.Match == "pedido_reiterado" {
				fmt.Println("\n*** PEDIDO REITERADO ***")
				fmt.Println("Foi encontrada ação idêntica nas ações extintas SEM resolução de mérito.")
				fmt.Printf("Comarca: %s\n", respPR.ComarcaNome)
				fmt.Printf("Vara: ID %d (%s)\n", respPR.VaraID, respPR.VaraAddr)
				fmt.Printf("Identificação da ação extinta: %s\n", respPR.AcaoID)
				fmt.Println("Será criada nova ação (novo número sequencial) na MESMA vara onde houve a extinção sem resolução de mérito.")

				createResp, err := criarAcaoNaVaraAddr(respPR.VaraAddr, "pedido_reiterado", respPR.AcaoID, nova, udpTimeout)
				if err != nil {
					fmt.Println("Erro ao criar ação por pedido reiterado:", err)
				} else if !createResp.Success {
					fmt.Println("Vara recusou criação de ação por pedido reiterado:", createResp.Message)
				} else {
					fmt.Printf("\nNova ação criada como PEDIDO REITERADO.\nIdentificação da nova ação: %s\n", createResp.AcaoID)
				}

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Aviso: falha ao verificar pedido reiterado nas varas locais:", err)
			}

			fmt.Println("4) Continência")
			// 4) CONTINÊNCIA
			respCont, err := consultarVarasLocalStage(vl, "continencia", nova, udpTimeout)

			// Se não encontrou nada localmente, consultar OUTRAS comarcas
			if respCont == nil || !respCont.Success || respCont.Match == "" || respCont.Match == "nenhuma" {
				respCont, err = consultarOutrasComarcasStage(nomeComarca, cl, "continencia", nova, udpTimeout)
				if err != nil {
					fmt.Println("Aviso: erro ao consultar outras comarcas para CONTINÊNCIA:", err)
				}
			}

			if respCont != nil && respCont.Success && (respCont.Match == "continencia_contida" || respCont.Match == "continencia_continente") {
				if respCont.Match == "continencia_contida" {
					fmt.Println("\n*** CONTINÊNCIA (AÇÃO CONTIDA) ***")
					fmt.Println("Foi encontrada ação CONTINENTE (pedido maior) com mesmas partes e mesma causa de pedir.")
					fmt.Printf("Comarca: %s\n", respCont.ComarcaNome)
					fmt.Printf("Vara: ID %d (%s)\n", respCont.VaraID, respCont.VaraAddr)
					fmt.Printf("Identificação da ação CONTINENTE: %s\n", respCont.AcaoID)
					fmt.Println("Não será criada nova ação, pois o pedido da nova ação é CONTIDO na ação CONTINENTE.")
				} else if respCont.Match == "continencia_continente" {
					fmt.Println("\n*** CONTINÊNCIA (AÇÃO CONTINENTE) ***")
					fmt.Println("Foi encontrada ação CONTIDA (pedido menor) com mesmas partes e mesma causa de pedir.")
					fmt.Printf("Comarca: %s\n", respCont.ComarcaNome)
					fmt.Printf("Vara: ID %d (%s)\n", respCont.VaraID, respCont.VaraAddr)
					fmt.Printf("Identificação da ação CONTIDA (a ser ampliada): %s\n", respCont.AcaoID)
					fmt.Println("As ações serão REUNIDAS, adicionando os pedidos da nova ação ao rol de pedidos da nova ação CONTINENTE.")

					_, err := enviarMergePedidosParaVaraAddr(respCont.VaraAddr, respCont.AcaoID, nova.Pedidos, udpTimeout)
					if err != nil {
						fmt.Println("Erro ao enviar merge de pedidos para a vara:", err)
					} else {
						fmt.Println("Pedidos da nova ação enviados para serem agregados à nova ação CONTINENTE (antiga ação CONTIDA).")
					}
				}

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Aviso: falha ao verificar continência nas varas locais:", err)
			}

			fmt.Println("5) Conexão")
			// 5) CONEXÃO
			respConx, err := consultarVarasLocalStage(vl, "conexao", nova, udpTimeout)

			// Se não encontrou nada localmente, consultar OUTRAS comarcas
			if respConx == nil || !respConx.Success || respConx.Match == "" || respConx.Match == "nenhuma" {
				respConx, err = consultarOutrasComarcasStage(nomeComarca, cl, "conexao", nova, udpTimeout)
				if err != nil {
					fmt.Println("Aviso: erro ao consultar outras comarcas para CONEXÃO:", err)
				}
			}

			if respConx != nil && respConx.Success && respConx.Match == "conexao" {
				fmt.Println("\n*** CONEXÃO ***")
				fmt.Println("Foi encontrada ação CONEXA (mesma causa de pedir e/ou mesmo(s) pedido(s)).")
				fmt.Printf("Comarca: %s\n", respConx.ComarcaNome)
				fmt.Printf("Vara: ID %d (%s)\n", respConx.VaraID, respConx.VaraAddr)
				fmt.Printf("Identificação da ação já existente: %s\n", respConx.AcaoID)
				fmt.Println("A nova ação será criada na MESMA vara, para julgamento conjunto (reunião por conexão).")

				createResp, err := criarAcaoNaVaraAddr(respConx.VaraAddr, "conexao", respConx.AcaoID, nova, udpTimeout)
				if err != nil {
					fmt.Println("Erro ao criar ação por conexão:", err)
				} else if !createResp.Success {
					fmt.Println("Vara recusou criação de ação por conexão:", createResp.Message)
				} else {
					fmt.Printf("\nNova ação criada como CONEXA.\nIdentificação da nova ação: %s\n", createResp.AcaoID)
					fmt.Println("A vara (lado servidor) deve registrar internamente a relação de ações conexas para julgamento conjunto.")
				}

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			if err != nil {
				fmt.Println("Aviso: falha ao verificar conexão nas varas locais:", err)
			}

			fmt.Println("6) Distribuição LIVRE")
			// 6) DISTRIBUIÇÃO LIVRE
			msg, err := distribuirAcaoLivre(nomeComarca, vl, nova, udpTimeout)
			if err != nil {
				fmt.Println("Erro ao realizar distribuição livre:", err)
			} else {
				fmt.Println()
				fmt.Println(msg)
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "2", "B", "b":
			// ---------- BUSCAR AÇÕES EM TODAS AS VARAS DA COMARCA ----------
			varas := vl.GetAll()
			if len(varas) == 0 {
				fmt.Println("Não há varas cadastradas nesta comarca.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			clearScreen()
			fmt.Println()
			fmt.Println("Buscar ações em TODAS as varas desta comarca.")
			fmt.Println("Buscar por:")
			fmt.Println("1 (I) - ID da ação")
			fmt.Println("2 (A) - Autor")
			fmt.Println("3 (R) - Réu")
			fmt.Println("4 (C) - Causa de pedir (número exato)")
			fmt.Println("5 (P) - Pedido (número exato)")
			fmt.Println("6 (S) - Retornar ao menu")
			fmt.Print("Sua opção> ")
			campoStr, _ := reader.ReadString('\n')
			campoStr = strings.TrimSpace(campoStr)

			var campo string
			switch campoStr {
			case "1", "I", "i":
				campo = "id"
			case "2", "A", "a":
				campo = "autor"
			case "3", "R", "r":
				campo = "reu"
			case "4", "C", "c":
				campo = "causa"
			case "5", "P", "p":
				campo = "pedido"
			case "6", "S", "s":
				clearScreen()
				continue
			default:
				fmt.Println("Opção de campo inválida.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Print("Valor para busca> ")
			val, _ := reader.ReadString('\n')
			val = strings.TrimSpace(val)
			if val == "" {
				fmt.Println("Valor de busca vazio.")
				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			fmt.Println("\nRealizando busca em todas as varas desta comarca...")
			totalEncontradas := 0

			for _, v := range varas {
				resp, err := buscarAcoesNaVara(v.Endereco, campo, val, udpTimeout)
				if err != nil {
					fmt.Printf("Aviso: falha ao buscar na Vara ID %d (%s): %v\n", v.ID, v.Endereco, err)
					continue
				}
				if !resp.Success {
					fmt.Printf("Aviso: Vara ID %d (%s) retornou erro: %s\n", v.ID, v.Endereco, resp.Message)
					continue
				}
				if len(resp.Resultados) == 0 {
					continue
				}

				varaID := resp.VaraID
				varaAddr := resp.VaraAddr
				if varaID == 0 {
					varaID = v.ID
				}
				if varaAddr == "" {
					varaAddr = v.Endereco
				}

				for _, r := range resp.Resultados {
					if totalEncontradas == 0 {
						fmt.Println("\n--- RESULTADOS DA BUSCA ---")
					}
					totalEncontradas++
					fmt.Printf("[Vara %d - %s] [%s] ID: %s | Autor: %s | Réu: %s | Causa: %d | Pedidos: %v\n",
						varaID, varaAddr,
						r.Lista,
						r.ID, r.Autor, r.Reu, r.CausaPedir, r.Pedidos)
				}
			}

			if totalEncontradas == 0 {
				fmt.Println("Nenhuma ação encontrada em nenhuma vara desta comarca.")
			} else {
				fmt.Printf("\nTotal de ações encontradas: %d\n", totalEncontradas)
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "3", "C", "c":
			fmt.Println("\nBuscando lista de comarcas no tribunal...")
			err := atualizarComarcasDoTribunal(*tribunalAddr, cl)
			if err != nil {
				fmt.Println("Não foi possível contactar o tribunal. Usando lista local.")
				log.Printf("Falha ao atualizar comarcas do tribunal: %v", err)
			} else {
				fmt.Println("Lista de comarcas atualizada a partir do tribunal.")
			}

			comarcas := cl.GetAll()
			if len(comarcas) == 0 {
				fmt.Println("(Nenhuma comarca na lista)")
			} else {
				fmt.Println("\n--- COMARCAS ---")
				for _, c := range comarcas {
					fmt.Printf("ID %d | %s | %s | %d varas\n",
						c.ID, c.Nome, c.Endereco, c.Varas)
				}
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "4", "V", "v":
			varas := vl.GetAll()
			if len(varas) == 0 {
				fmt.Println("(Nenhuma vara cadastrada para esta comarca)")
			} else {
				fmt.Println("\n--- VARAS ---")
				for _, v := range varas {
					fmt.Printf("ID %d | Endereço UDP: %s\n", v.ID, v.Endereco)
				}
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "5", "A", "a":
			fmt.Print("Endereço UDP da nova vara (ex: 127.0.0.1:9201): ")
			endStr, _ := reader.ReadString('\n')
			endStr = strings.TrimSpace(endStr)
			if endStr == "" {
				fmt.Println("Endereço inválido.")

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			v, err := vl.Add(endStr)
			if err != nil {
				fmt.Println("Erro ao adicionar vara:", err)
				log.Printf("Erro ao adicionar vara: %v", err)

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}
			fmt.Println()
			fmt.Printf("Vara adicionada: ID %d, endereço %s\n", v.ID, v.Endereco)

			totalVaras := vl.Count()
			if err := enviarUpdateVaras(*tribunalAddr, nomeComarca, totalVaras); err != nil {
				fmt.Println("Aviso: não foi possível notificar o tribunal sobre o novo número de varas.")
				log.Printf("Erro ao enviar update_varas ao tribunal: %v", err)
			} else {
				fmt.Println("Tribunal notificado sobre o novo número de varas.")
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "6", "D", "d":
			fmt.Print("ID da vara a remover: ")
			idStr, _ := reader.ReadString('\n')
			idStr = strings.TrimSpace(idStr)
			id, err := strconv.Atoi(idStr)
			if err != nil {
				fmt.Println("ID inválido.")

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}

			v, err := vl.RemoveByID(id)
			if err != nil {
				fmt.Println("Erro ao remover vara:", err)
				log.Printf("Erro ao remover vara: %v", err)

				fmt.Print("\nPressione ENTER para voltar ao menu...")
				reader.ReadString('\n')
				clearScreen()
				continue
			}
			fmt.Println()
			fmt.Printf("Vara removida: ID %d, endereço %s\n", v.ID, v.Endereco)

			totalVaras := vl.Count()
			if err := enviarUpdateVaras(*tribunalAddr, nomeComarca, totalVaras); err != nil {
				fmt.Println("Aviso: não foi possível notificar o tribunal sobre o novo número de varas.")
				log.Printf("Erro ao enviar update_varas ao tribunal: %v", err)
			} else {
				fmt.Println("Tribunal notificado sobre o novo número de varas.")
			}

			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()

		case "7", "S", "s":
			// Sair
			if err := vl.Save(); err != nil {
				log.Printf("Erro ao salvar varas ao sair: %v", err)
			}
			if err := cl.Save(); err != nil {
				log.Printf("Erro ao salvar comarcas ao sair: %v", err)
			}
			salvarNomeComarca(nomeComarcaFile, nomeComarca)
			salvarEnderecoComarca(addrComarcaFile, comarcaAddr)
			fmt.Println("Dados salvos. Encerrando comarca.")
			return

		default:
			fmt.Println("Opção inválida.")
			fmt.Print("\nPressione ENTER para voltar ao menu...")
			reader.ReadString('\n')
			clearScreen()
		}
	}
}
