# AGENTS.md - Diretrizes para Agentes de IA no Combobreak

Este documento serve como guia de contexto, arquitetura, padrões e comandos para agentes autônomos de IA e desenvolvedores que trabalham no repositório **Combobreak**.

---

## 1. Visão Geral do Projeto

O **Combobreak** é uma ferramenta de sincronização em Go projetada para interagir com a API do **OmniRoute**. Ele consulta periodicamente (ou sob demanda) os modelos disponíveis na rota `/v1/models` e filtra aqueles que cumprem cumulativamente três critérios essenciais:
1. **Tool Calling / Function Calling**: capacidade nativa de invocar ferramentas.
2. **Reasoning / Thinking**: suporte a raciocínio avançado / cadeia de pensamento.
3. **Free Tier**: modelos gratuitos (identificados por tags no nome/ID ou por precificação zerada).

Com os modelos qualificados, ele cria ou atualiza um combo de roteamento (por padrão chamado `combobreak`) na rota `/api/combos` do OmniRoute.

### Princípio Arquitetural Chave
- **Zero Dependências Externas**: O projeto utiliza exclusivamente a biblioteca padrão do Go (`stdlib`). Evite adicionar bibliotecas de terceiros a menos que explicitamente solicitado.

---

## 2. Estrutura do Repositório

```text
combobreak/
├── config/
│   ├── config.go         # Carregamento de flags CLI, variáveis de ambiente e arquivo .env
│   └── config_test.go    # Testes unitários do carregamento de configurações
├── omniroute/
│   ├── client.go         # Cliente HTTP para a API OmniRoute (/v1/models e /api/combos)
│   ├── client_test.go    # Testes unitários e mocks do cliente HTTP
│   ├── filter.go         # Lógica central de seleção (Tool Calling + Reasoning + Free)
│   ├── filter_test.go    # Casos de teste para verificação de modelos e regras de filtro
│   └── models.go         # DTOs, structs e tipos auxiliares (ex: FlexBool, Capabilities)
├── main.go               # Ponto de entrada, orquestração CLI e loop contínuo (watcher)
├── main_test.go          # Testes de integração end-to-end com httptest.Server
├── Makefile              # Tarefas automatizadas de build, test e execução
├── go.mod                # Módulo Go puro (sem requires externos)
└── README.md             # Documentação de usuário final
```

---

## 3. Comandos de Desenvolvimento

Todos os comandos devem ser executados a partir da raiz do repositório:

| Tarefa | Comando Makefile | Comando Nativo Go |
| :--- | :--- | :--- |
| **Executar Testes** | `make test` | `go test -v ./...` |
| **Compilar Binário** | `make build` | `go build -o combobreak .` |
| **Executar Localmente** | `make run` | `go run main.go` |
| **Limpar Binários** | `make clean` | `rm -f combobreak` |
| **Formatar Código** | — | `go fmt ./...` |
| **Análise Estática (Vet)** | — | `go vet ./...` |

> **Atenção**: Sempre execute `go test -v ./...` antes de finalizar qualquer edição para assegurar que nenhum teste foi quebrado.

---

## 4. Regras de Negócio e Critérios de Filtro

Ao modificar a lógica do pacote `omniroute`, preste atenção às seguintes regras implementadas em `filter.go`:

1. **Auto-exclusão**:
   - Modelos cujo ID corresponda ao próprio nome do combo (ex: `combobreak`) devem ser ignorados para evitar loops de roteamento recursivo.
2. **Tool Calling (`HasToolCalling`)**:
   - Checa `Capabilities.ToolCalling`, `Capabilities.Tools` ou `Capabilities.FunctionCalling`.
3. **Reasoning (`HasReasoning`)**:
   - Checa `Capabilities.Reasoning`, `Capabilities.Thinking`, `Capabilities.SupportsThinking` ou a presença de `EffortTiers`.
4. **Gratuito (`IsFreeModel`)**:
   - Identificado se o ID, Name ou Root terminar com sufixo gratuito (ex: `:free`, `-free`).
   - Identificado se `Pricing.Input == 0` e `Pricing.Output == 0` (quando informados).
   - Identificado se `IsFree == true`.
5. **Combos no OmniRoute**:
   - O cliente primeiro tenta listar ou obter o combo via `GET /api/combos` ou `GET /api/combos/{name}`.
   - Em caso de status 404, realiza `POST /api/combos`. Caso já exista, realiza `PUT /api/combos/{name}` ou `PUT /api/combos`.
   - Aceita a flag `-dry-run` para apenas simular a avaliação sem disparar alterações na API.

---

## 5. Padrões de Código para Agentes

1. **Tratamento de Erros**:
   - Utilize embrulho de erros com `%w` (`fmt.Errorf("falha ao ...: %w", err)`).
   - Mensagens de log seguem os prefixos convencionais: `[INFO]`, `[WARN]`, `[ERROR]`, `[FATAL]`.
2. **Context & Concorrência**:
   - Todas as requisições HTTP e operações de longa duração devem receber `context.Context`.
   - Respeite o encerramento gracioso via sinais `SIGINT` e `SIGTERM` em `main.go`.
3. **Testes Unitários & Mocks**:
   - Utilize `net/http/httptest` para criar servidores de mock da API OmniRoute nos testes.
   - Ao adicionar novos campos a `omniroute.ModelEntry` ou `Capabilities`, adicione cenários correspondentes em `omniroute/filter_test.go`.
4. **Sem Dependências Desnecessárias**:
   - Não adicione bibliotecas externas (como frameworks HTTP, loggers pesados ou libs de assertion). Mantenha `go.mod` limpo apenas com a stdlib.

---

## 6. Fluxo de Git e Branches

- **Trabalho Direto na `main`**: Não crie nem abra novas branches a menos que seja explicitamente requisitado pelo usuário. Todo o desenvolvimento, correções e commits devem ser realizados diretamente na branch `main`.

