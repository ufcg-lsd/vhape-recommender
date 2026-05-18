# Vhape — Documentação de mudanças em relação ao Kubernetes VPA padrão

Este documento é um guia para revisores de código. Descreve todos os arquivos adicionados ou modificados em relação ao recommender upstream do Kubernetes VPA (`registry.k8s.io/autoscaling/vpa-recommender`), explicando o que cada mudança faz e por que foi feita.

---

## Mudança arquitetural central

No VPA padrão, todos os parâmetros do recommender (percentil alvo, margem de segurança, mínimos de CPU/memória) são configurados via **flags de linha de comando** no deployment YAML. Isso significa que qualquer mudança de configuração exige reiniciar o pod do recommender, e todos os namespaces do cluster usam os mesmos parâmetros.

No vhape, esses parâmetros foram movidos para um **CRD chamado `VhapePolicy`**, aplicado por namespace. Isso permite que cada namespace ou workload tenha sua própria heurística, headroom e regras de escalonamento, sem reiniciar o recommender.

---

## Arquivos novos

### `pkg/recommender/logic/vhape_policy.go`

**O que faz:** Define os tipos Go que representam o CRD `VhapePolicy` em memória, e a função que busca esse objeto no cluster.

**Por que foi criado:** Para que o recommender consiga ler a configuração de cada VPA dinamicamente a cada ciclo, em vez de depender de flags fixas no deployment. Cada VPA anota qual policy deve usar (`vhape/policy: <nome>`), e o recommender busca essa policy antes de calcular a recomendação.

**Detalhes relevantes:**
- `VhapePolicy`, `VhapePolicySpec`, `VhapeResourceConfig` — structs que representam o CRD em memória após a leitura do cluster
- `FetchVhapePolicy(client, namespace, name)` — busca o objeto `VhapePolicy` no namespace `kube-system` usando o dynamic client. Retorna erro se a anotação `vhape/policy` não estiver definida no VPA ou se o objeto não for encontrado no cluster. Nesse caso, o VPA é ignorado naquele ciclo
- Valores padrão (percentile=0.93, headroom=0.10, bounds=0.10) são aplicados para qualquer campo não definido no CRD

---

### `pkg/recommender/logic/heuristics/hysteresis.go`

**O que faz:** Implementa os estimadores de janela deslizante de 24 horas para CPU e memória.

**Por que foi criado:** O VPA padrão usa um `DecayingHistogram` — um histograma com decaimento exponencial que dá mais peso às amostras recentes. O vhape substitui isso por uma janela deslizante simples: todas as amostras das últimas 24 horas têm peso igual, e amostras mais antigas são descartadas. O comportamento é mais previsível e direto: a recomendação reflete exatamente o percentil dos últimos 24 horas de uso real.

**Por que os métodos foram separados em `FeedSamples` e `GetEstimation`:**
O VPA usa o padrão decorator — estimadores são empilhados em camadas (ex: `cpuMarginEstimator` envolve o estimador base e adiciona o headroom). Esses decoradores chamam o estimador interno passando `nil` para os dados de uso corrente. Se a coleta de amostras e o cálculo do percentil ficassem no mesmo método, as amostras nunca seriam coletadas quando o método fosse chamado através de um decorator. A solução foi separar em dois métodos:
- `FeedSamples(containerName, []float64)` — chamado diretamente no loop principal do recommender, antes da cadeia de decoradores; adiciona as amostras do ciclo atual, descarta amostras com mais de 24h e executa GC de containers que não recebem amostras há mais de 24h
- `GetCPUEstimation / GetMemoryEstimation` — chamado pelos decoradores; apenas lê e calcula o percentil sobre as amostras já armazenadas, sem efeitos colaterais

**Outros detalhes:**
- Valores mínimos garantidos: 25 millicores de CPU, 250 MB de memória
- Um `sync.Pool` (`percentileBufPool`) reutiliza o buffer temporário de ordenação para evitar alocações grandes a cada ciclo de recomendação

---

### `pkg/recommender/logic/types/types.go`

**O que faz:** Pacote com tipos compartilhados entre os pacotes `logic`, `scaling_rules` e outros.

**Por que foi criado:** Para evitar ciclos de importação. O pacote `scaling_rules` precisa conhecer `RecommendedContainerResources` e `ScalingRuleContext`, mas não pode importar o pacote `logic` diretamente (que é quem o usa). Extrair esses tipos para um pacote separado resolve o ciclo.

- `RecommendedContainerResources` — agrupa os três mapas de recursos (target, lowerBound, upperBound) de uma recomendação
- `ScalingRuleContext` — carrega o nome do container e os requests atuais de CPU e memória, necessários para as regras de escalonamento decidirem se devem bloquear ou ajustar a recomendação

---

### `pkg/recommender/logic/scaling_rule.go`

**O que faz:** Define a interface `ScalingRule` e a função que seleciona qual regra está ativa com base na `VhapePolicy`.

**Por que foi criado:** Para permitir controlar a direção do escalonamento via policy, sem alterar os estimadores. Uma scaling rule é aplicada após os estimadores calcularem a recomendação, podendo bloquear scale-up, scale-down, ou deixar passar.

- `ScalingRule.Apply(recommendation, ctx)` — recebe a recomendação calculada e o contexto do container (request atual), e retorna a recomendação possivelmente modificada
- `selectScalingRule(policy)` — retorna a regra correspondente ao campo `scalingRule` da policy; retorna `nil` se nenhuma estiver configurada, e a recomendação passa sem modificação

---

### `pkg/recommender/logic/scaling_rules/scaledown_only.go`

**O que faz:** Implementa a regra `scale-down-only`.

**Por que foi criado:** Para workloads onde só se deseja reduzir recursos, nunca aumentar. Se a recomendação for maior que o request atual, ela é substituída pelo valor atual, impedindo que o VPA aumente o request.

- Se `target > request atual`: target e upperBound são limitados ao request atual
- Se `target <= request atual`: a recomendação passa sem alteração

---

### `pkg/recommender/logic/scaling_rules/scaleup_only.go`

**O que faz:** Implementa a regra `scale-up-only`.

**Por que foi criado:** Para workloads onde só se deseja aumentar recursos, nunca reduzir. Se a recomendação for menor que o request atual, ela é substituída pelo valor atual, impedindo que o VPA reduza o request.

- Se `target < request atual`: target e lowerBound são limitados ao request atual
- Se `target >= request atual`: a recomendação passa sem alteração

---

## Arquivos modificados

### `pkg/recommender/logic/estimator.go`

**Mudanças nas assinaturas das interfaces `CPUEstimator` e `MemoryEstimator`:**

No VPA padrão, os métodos recebem apenas `*AggregateContainerState`. No vhape, recebem também `containerName string` e `currentUsages []float64`.

- `containerName` foi adicionado porque o `HysteresisCPUEstimator` mantém amostras separadas por container — ele precisa saber qual container está sendo consultado para retornar o percentil correto
- `currentUsages` foi adicionado como extensão da interface para possíveis heurísticas futuras que precisem dos valores de uso do ciclo atual; todos os decoradores existentes ignoram esse parâmetro (`_ []float64`) e os decoradores propagam `containerName` pela cadeia até chegar no estimador base

**Novos tipos adicionados ao final do arquivo:**

- **Constante `HeuristicPercentileHysteresis`** — nome da única heurística ativa atualmente (`"percentile-hysteresis"`). Usada no campo `spec.heuristic` da `VhapePolicy` e no `selectHeuristic` para identificar qual estimador criar

- **Struct `cachedEstimators`** — agrupa todas as instâncias de estimadores (2 base hysteresis + 6 decorados) de uma única policy. É mantido em memória entre ciclos de recomendação. Sem esse cache, os estimadores seriam recriados a cada ciclo e todo o histórico de amostras da janela deslizante seria perdido

- **`selectHeuristic(policy)`** — factory que cria o `cachedEstimators` a partir de uma `VhapePolicy`. Usa o padrão decorator do VPA para montar os estimadores de target, lowerBound e upperBound aplicando headroom e bounds configurados na policy:
  - `target = base * (1 + headroom)`
  - `lowerBound = base * (1 - lb) * (1 + headroom)`
  - `upperBound = base * (1 + ub) * (1 + headroom)`
  - O switch com `default` foi mantido intencionalmente para facilitar a adição de novas heurísticas no futuro

---

### `pkg/recommender/logic/recommender.go`

**Struct `podResourceRecommender` — completamente substituído:**

No VPA padrão, o struct guardava diretamente os 6 estimadores (target/lower/upper para CPU e memória), que eram construídos uma vez na inicialização com base nas flags.

No vhape, o struct precisa ser capaz de buscar configuração dinâmica do cluster e manter estado por policy. Os campos são:

- **`dynamicClient dynamic.Interface`** — cliente Kubernetes que permite ler CRDs arbitrários sem precisar de um tipo Go gerado. Foi necessário porque `VhapePolicy` é um CRD customizado do vhape, não conhecido pelo cliente tipado padrão do Kubernetes. É criado a partir do kubeconfig em `main.go` e passado para o recommender
- **`metricsClient input_metrics.MetricsClient`** — cliente de métricas já existente no VPA padrão (usado pelo `cluster_feeder` para alimentar o histograma). No vhape, uma referência ao mesmo cliente é passada também para o recommender, para que o `HysteresisCPUEstimator` possa coletar as amostras do ciclo atual via `GetContainersMetrics()`. Os dois usos são independentes: o `cluster_feeder` alimenta o histograma, o recommender alimenta a janela deslizante
- **`mu sync.Mutex`** — protege o mapa `estimators` de acessos concorrentes
- **`estimators map[string]*cachedEstimators`** — cache de estimadores por policy, com chave `namespace/nome-da-policy`. Garante que o histórico de amostras persista entre ciclos

**`GetRecommendedPodResources` — estendido com `namespace` e `annotations`:**

No VPA padrão, o método recebe apenas o mapa de containers. No vhape, recebe também o namespace do VPA e suas anotações, necessários para:
1. Ler a anotação `vhape/policy` e resolver o nome da policy
2. Buscar a `VhapePolicy` correspondente no cluster
3. Filtrar as métricas pelo namespace correto ao montar os mapas de uso por container

**`estimateContainerResources`** — simplificado para espelhar o padrão do upstream: chama cada estimador no `cachedEstimators`, aplica `FilterControlledResources` e depois aplica a `ScalingRule` ativa se houver uma configurada.

**`CreatePodResourceRecommender`** — agora recebe `dynamicClient` e `metricsClient` em vez de construir estimadores a partir de flags. Os estimadores são criados sob demanda no primeiro ciclo de cada policy.

**`MapToListOfRecommendedContainerResources`** — agora recebe um struct `RecommendationFormat` em vez de ler variáveis globais de flags, tornando o comportamento de formatação explícito e testável.

**`RecommendationConfig`** — herdado do upstream como código morto. No VPA padrão era usado para passar os parâmetros de configuração para o recommender. No vhape, toda configuração vem da `VhapePolicy`, então esse struct não é utilizado.

---

### `pkg/recommender/routines/recommender.go`

- **`checkpointsWriteTimeout`** — no upstream era uma flag de pacote lida globalmente. No vhape foi movida para campo do struct `recommender` e configurada via `RecommenderConfig`. Isso centraliza a configuração em `main.go` e evita dependência de variáveis globais
- **`recommendationFormat logic.RecommendationFormat`** — adicionado ao struct interno para que `processVPAUpdate` possa passar o formato de saída para `MapToListOfRecommendedContainerResources` sem depender de flags globais
- **`processVPAUpdate`** — estendido para passar `observedVpa.Namespace` e `observedVpa.Annotations` para `GetRecommendedPodResources`, que precisa dessas informações para buscar a `VhapePolicy` correta
- **Flag `MinCheckpointsPerRun`** — removida junto com o aviso de deprecação, pois era marcada como deprecated no upstream e não tinha efeito real

---

### `pkg/recommender/main.go`

- **`dynamicClient`** — criado via `dynamic.NewForConfigOrDie(config)` a partir do kubeconfig do cluster. Passado para `CreatePodResourceRecommender` para que o recommender consiga buscar objetos `VhapePolicy` do cluster a cada ciclo
- **`metricsClient`** — no upstream era criado inline dentro da configuração do `ClusterStateFeeder`. No vhape foi extraído para uma variável local e passado para dois lugares: o `ClusterStateFeeder` (que alimenta o histograma, como antes) e o `CreatePodResourceRecommender` (que alimenta a janela deslizante). Os dois usos são independentes
- **Flags de formato** (`--humanize-memory`, `--round-cpu-millicores`, `--round-memory-bytes`) — no upstream estavam definidas como variáveis globais em `logic/recommender.go`. No vhape foram movidas para `main.go` e conectadas ao struct `RecommendationFormat`, que é passado explicitamente pela cadeia de chamadas
- **`--checkpoints-timeout`** — movida do pacote `routines` para `main.go` para centralizar todas as flags em um único lugar
- **Bloco de deprecação do `--min-checkpoints`** — removido junto com a flag, pois não tinha efeito real

---

## Como aplicar uma VhapePolicy

Todo objeto VPA que deve usar o vhape **precisa** ter a anotação `vhape/policy: <nome>` apontando para um objeto `VhapePolicy` no namespace `kube-system`. Se a anotação estiver ausente ou o objeto não for encontrado, o VPA é ignorado naquele ciclo.

Exemplo:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapePolicy
metadata:
  name: minha-policy
  namespace: kube-system
spec:
  heuristic: percentile-hysteresis
  cpu:
    percentile: 0.93
    headroom: 0.10
    lowerBound: 0.10
    upperBound: 0.20
  memory:
    percentile: 0.93
    headroom: 0.10
    lowerBound: 0.10
    upperBound: 0.20
  scalingRule: ""   # ou "scale-down-only" / "scale-up-only"
```
