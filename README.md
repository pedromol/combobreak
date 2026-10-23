# Combobreak

Aplicação em Golang para sincronizar e gerenciar combos no **OmniRoute**.

O **Combobreak**:
1. Consulta a API `/v1/models` do OmniRoute para listar todos os modelos disponíveis.
2. Filtra e seleciona os modelos que atendem cumulativamente a:
   - **Capacidade de Tool Calling**: suporte nativo a ferramentas/funções (`tool_calling`, `tools`, `function_calling`).
   - **Capacidade de Reasoning**: suporte a raciocínio/pensamento profundo (`reasoning`, `thinking`, `supportsThinking`, `effort_tiers`).
   - **Modelo Free**: indicado como gratuito no nome/ID/root (ex: `:free`, `-free`) ou no custo informado (`pricing.input = 0` e `pricing.output = 0`, ou `is_free = true`).
3. Consulta e atualiza/cria o combo chamado **`combobreak`** na API `/api/combos` com os modelos filtrados.

---

## ⚙️ Configurações

A aplicação aceita variáveis de ambiente e flags de linha de comando:

| Variável de Ambiente | Flag CLI | Padrão | Descrição |
| :--- | :--- | :--- | :--- |
| `OMNIROUTE_BASE_URL` | `-base-url` | `http://localhost:20128` | URL base da instância do OmniRoute |
| `OMNIROUTE_API_KEY` | `-api-key` | *(vazio)* | Token/chave de autorização Bearer do OmniRoute |
| `COMBO_NAME` | `-combo-name` | `combobreak` | Nome do combo criado ou atualizado |
| `COMBO_STRATEGY` | `-strategy` | `context-optimized` | Estratégia de roteamento do combo (`context-optimized`, `priority`, `round-robin`, etc.) |
| — | `-dry-run` | `false` | Apenas simula e lista os modelos que seriam adicionados |
| — | `-verbose` | `false` | Exibe logs detalhados de avaliação de cada modelo |
| — | `-interval` | `0` *(executa uma vez)* | Intervalo de sincronização contínua (ex: `30m`, `1h`) |
| — | `-timeout` | `30s` | Timeout para requisições HTTP |

> **Dica**: Você também pode criar um arquivo `.env` na raiz do projeto com as variáveis.

---

## 🚀 Como Executar

### Pré-requisitos
- Go 1.22+ instalado

### 1. Execução Direta

```bash
# Definindo as variáveis no ambiente
export OMNIROUTE_BASE_URL="http://localhost:20128"
export OMNIROUTE_API_KEY="seu_token_aqui"

# Executar
go run main.go
```

Ou passando via flags:

```bash
go run main.go -base-url="http://localhost:20128" -api-key="seu_token_aqui" -verbose
```

### 2. Modo Dry-Run (Pré-visualização)

Para checar quais modelos são detectados sem alterar nada no OmniRoute:

```bash
go run main.go -dry-run -verbose
```

### 3. Modo Contínuo (Daemon / Watcher)

Para manter o combo sempre atualizado periodicamente (por exemplo, a cada 1 hora):

```bash
go run main.go -interval=1h
```

### 4. Compilar o Binário

```bash
make build
# ou
go build -o combobreak .

./combobreak
```

---

## 🧪 Testes

Execute a suíte de testes unitários e de integração:

```bash
make test
# ou
go test -v ./...
```

---

## 📂 Estrutura do Projeto

```
combobreak/
├── config/             # Leitura de variáveis de ambiente, flags e .env
│   ├── config.go
│   └── config_test.go
├── omniroute/          # Cliente HTTP OmniRoute, DTOs e lógica de filtro
│   ├── client.go       # Chamadas a /v1/models e /api/combos (Create/Update/Upsert)
│   ├── client_test.go
│   ├── filter.go       # Filtro de Tool Calling, Reasoning e Free
│   ├── filter_test.go
│   └── models.go       # Modelos de dados e serialização JSON flexível
├── main.go             # Ponto de entrada, orquestração e modo watcher
├── main_test.go        # Teste de integração end-to-end com mock server
├── Makefile
└── README.md
```
