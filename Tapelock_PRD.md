Tapelock — PRD technique & architecture v0.3
Statut : draft de conception · Nom : Tapelock, disponibilité à vérifier (voir §0.1) · Licence recommandée : Apache-2.0 pour le CLI, SaaS propriétaire dans un repo séparé.

Changelog
v0.3 : renommage Driftlock → Tapelock (§0.1). Aucun autre changement fonctionnel.

v0.2 (par rapport à v0.1) :

Nom : Fixed (trop générique) → Driftlock (abandonné : collisions).
Topologie 3 apps (CLI, API, dashboard) intégrée, avec phasage révisé (§2.0).
SaaS repoussé de M2 à M4+, derrière un gate de validation (§4).
S1 = OpenAI Chat Completions uniquement ; Anthropic passe en Layer 2 (interface Provider dès J1).
Structure Go : découpage pur / I/O au lieu de 4 couches (§2.1).
Corrections : lib JSON Schema à choisir (pas dans la stdlib), verdict unknown si usage absent, garde-fou anti-drift sur PR.
Principe 6 : le SaaS reçoit des rapports, pas des payloads.
0. Principes non négociables
Local-first : le CLI ne dépend jamais du SaaS pour fonctionner. Un SaaS injoignable ne fait jamais échouer un build.
Déterminisme : aucun LLM juge. Toute assertion est une fonction pure de (cassette, config).
Fail-closed en CI : cassette illisible, version inconnue ou miss en --strict = échec. Jamais de fallback silencieux vers l'API payante.
Deux voies CI séparées :
PR lane : replay strict, gratuite, zéro flake. Détecte les régressions de ton code ou de tes prompts.
Drift lane : appels live, statistique, budgétée. Détecte les régressions du modèle.
Cassette = contrat versionné (v), diffable en revue de code.
Rapports, pas payloads : le SaaS ingère des résultats agrégés et des hash, jamais les prompts. Zéro PII côté serveur tant que le Vault n'existe pas.
0.1 Nom : Tapelock
Historique : Fixed (trop générique) → Driftlock (déjà très utilisé : paquet PyPI de gouvernance de coûts LLM, plusieurs CLI GitHub, CLI npm @drift-lock/cli) → Tapelock.
Sens : tape = la cassette (record/replay), lock = la baseline verrouillée (drift). Couvre le CLI OSS et la couche SaaS.
Signal initial : une recherche rapide n'a fait remonter aucun projet de ce nom. Signal faible, pas une preuve.
Risques de perception : « tape » peut évoquer les bandes de sauvegarde, « lock » le verrouillage de fichiers.
À vérifier avant tout achat de domaine ou publication :
npm, PyPI, crates.io, GitHub (org + repo), Homebrew.
Domaine .dev ou .io.
Recherche « tapelock LLM » : si un outil IA apparaît en première page, abandonner.
Marque : EUIPO/USPTO (clients internationaux) et OAPI (Cameroun).
Nom en constante unique (renommage peu coûteux avant v0.1) : binaire tapelock, dossier .tapelock/, config tapelock.yaml, module Go, préfixe d'env TAPELOCK_*, headers x-tapelock-*.
Repli si collision sérieuse : Pinreel, Baselatch (non vérifiés).
1. PRD fonctionnel
1.1 MVP — Semaine 1 (CLI Go OSS, aucune infra)
Hors périmètre S1 : assertions, drift, Anthropic/Gemini, Responses API, backend, dashboard, Vault, PII avancée.

Providers : OpenAI Chat Completions (+ streaming SSE) uniquement. Les endpoints compatibles (OpenRouter, Groq, vLLM, Ollama…) sont à valider au cas par cas via upstream configurable. Interface Provider définie dès J1 pour ne pas réécrire à l'arrivée d'Anthropic.

Commandes
Commande	Comportement
tapelock record -- <cmd>	Proxy sur port éphémère (127.0.0.1), injecte OPENAI_BASE_URL dans l'env de <cmd>, forward vers l'upstream, écrit les cassettes
tapelock replay -- <cmd>	Hit : sert la cassette. Miss : forward + enregistre, avec warning
tapelock replay --strict -- <cmd>	Miss : erreur au format du provider (header x-tapelock-miss: 1), exit 3 en fin de run
tapelock proxy --mode record|replay	Mode serveur long-running (dev local)
tapelock cassette ls|show|prune	Inspection et GC
✅ Le wrapper -- <cmd> évite les conflits de port et garantit l'injection d'env / ⚠️ il ne couvre pas les apps à base URL codée en dur → garder proxy.
--explain-miss (à prévoir tôt) : diff canonique entre la requête reçue et la cassette la plus proche. Sans elle, le premier miss inexpliqué fait désinstaller l'outil.
Exit codes : 0 ok · 1 assertion en échec · 2 config invalide · 3 miss en strict · 4 drift détecté · 5 cassette invalide / version inconnue.

Moteur de matching (canonicalisation + hash)
Pipeline : adapter provider → sélection des champs → normalizers → JCS (RFC 8785) → SHA-256 → clé + compteur d'occurrence.

Champs inclus (OpenAI chat) : model, messages, tools, tool_choice, response_format, temperature, top_p, seed, max_tokens / max_completion_tokens, stop, stream, stream_options.
Exclus : user, metadata, headers, IDs de requête.
Occurrence : compteur par clé (boucles d'agents avec requêtes identiques et réponses différentes).
Normalizers (appliqués au hash uniquement ; la cassette garde le body d'origine redacté) : built-in uuid, iso8601, unix_ts, hex_id ; custom {path (JSONPath), regex, replace}.
JCS : encoding/json n'est pas canonique. Utiliser une lib JCS éprouvée + vecteurs du RFC 8785 (piège : flottants, 1.0 vs 1).
Pièges :
Écho : si le modèle recopie l'UUID/la date du prompt, la réponse rejouée contient l'ancienne valeur → faux vert possible.
Sur-normalisation : prompts distincts mappés sur la même clé = faux hits → --audit-normalizers.
✅ normalizers = hit rate élevé / ⚠️ risque de faux hits.
Proxy SSE
Handler custom + http.Flusher (ou ReverseProxy avec FlushInterval: -1). Aucun buffering.
Upstream en Accept-Encoding: identity.
Record : tee streaming ; chaque frame (\n\n) est envoyée au client puis journalisée brute avec dt_ms. Pas de re-sérialisation. Écriture disque asynchrone, mémoire bornée.
Déconnexion client en plein stream : annuler le contexte upstream (sinon tokens payés pour rien), marquer complete: false → jamais rejouable.
stream_options.include_usage : le proxy ne l'injecte jamais (ça changerait le hash). Si usage est absent, les assertions de tokens rendent unknown (configurable), jamais pass.
Replay : headers utiles (allowlist), [DONE] préservé, transfert chunked, timing instant | recorded | scaled:<f>.
Erreurs upstream (429/5xx) : passthrough, non enregistrées par défaut (--record-errors pour opt-in).
Sécurité (OWASP) : bind 127.0.0.1 ; upstream en allowlist (pas de proxy ouvert / SSRF) ; headers d'auth (authorization, x-api-key, openai-organization) jamais persistés, redaction gratuite et par défaut.
Test de contrat CI : rejouer une cassette avec le SDK officiel OpenAI Node ; critère = flux consommé sans erreur, identique octet pour octet.
1.2 Layer 2 — Mois 1-2 (Drift & CI Guard, toujours sans backend)
Ajouts : adapter Anthropic, moteur d'assertions, drift, GitHub Action, rapport de PR.

Assertions déterministes
Assertion	Règle	Note
schema	JSON Schema (draft 2020-12) sur le contenu parsé	Voir lib et note Zod
tool_calls	Noms en allowlist, arguments validés par schéma	Arguments fragmentés en SSE → reducer par provider depuis les events bruts
finish_reason	Dans une liste autorisée	OpenAI : stop, tool_calls, length. Anthropic : end_turn, tool_use, max_tokens
latency	max_ttfb_ms, max_total_ms	Préférer le TTFB, plus stable
usage	max_prompt_tokens, max_completion_tokens, max_total_tokens	Assertion primaire de coût. Noms normalisés dans le domaine (prompt_tokens OpenAI ↔ input_tokens Anthropic)
cost	max_usd = tokens × table de prix versionnée	Secondaire, avertissement seulement (la table se périme)
Lib JSON Schema en Go : absente de la stdlib. Critères : draft 2020-12, résolution des $ref sans accès réseau (déterminisme + SSRF), erreurs structurées exploitables par le rapport.
Zod : le CLI Go ne l'exécute pas. Un helper npm compile Zod vers des fichiers JSON Schema (Zod v4 : z.toJSONSchema() ; v3 : zod-to-json-schema). ✅ un seul moteur de validation / ⚠️ une étape de build en plus. Python (Pydantic model_json_schema()) viendra ensuite : le CLI n'est pas pénalisé, seuls le helper et la doc changent.
Les assertions tournent dans les deux voies : PR lane (le nouveau code accepte-t-il les sorties enregistrées ?) et drift lane (le modèle respecte-t-il encore le contrat ?).
Re-record planifié et détection de drift
tapelock drift run rejoue les requêtes des cassettes contre l'upstream réel, N échantillons chacune (défaut 5), avec plafond --budget-usd.
Fingerprint (jamais du texte libre) : forme JSON (ensemble trié de path:type), noms de tool calls, finish_reason, buckets d'usage (log2), model_served.
Baseline : N ≥ 5 échantillons, stockée en git, modifiée uniquement par tapelock baseline accept (comme -u en snapshot testing).
Déclencheurs : cron nocturne, workflow_dispatch, changement de fichiers prompts/schémas (paths). Jamais à chaque PR.
Garde-fou : drift run refuse de s'exécuter si GITHUB_EVENT_NAME=pull_request, sauf --allow-pr. Documenter ne suffit pas : la première équipe qui mélange les deux voies brûle son budget.
Attribution de cause : « model_served a changé » vs « même modèle, comportement changé ».
Changement de prompt : la PR lane échoue en strict (« miss : relancer tapelock record »). Le dev re-record en local et commite. Pas de bot de re-record sur les forks (secrets indisponibles).
Rapport de PR (sans backend)
tapelock report --format md|json|junit.
L'Action commente directement avec GITHUB_TOKEN (pull-requests: write). Un webhook serveur n'est pas nécessaire et ajouterait une GitHub App à opérer.
Commentaire sticky unique, mis à jour à chaque run (marqueur <!-- tapelock-report -->).
Contenu : verdict par suite, misses, delta tokens/latence vs baseline, alertes drift (tier 2 en warning).
2. Architecture
2.0 Topologie et phasage
PHASE 1 (S1) : locale, zéro infra
  [App dev/tests] -> [tapelock proxy] -> [Upstream LLM]
                          |
                          v
                 [Cassettes .jsonl dans git]

PHASE 2 (M1-2) : + CI, toujours zéro backend
  [GitHub Action] -> tapelock (record/replay/drift/report)
        |-> commentaire PR sticky (GITHUB_TOKEN)
        '-> issue / alerte sur drift

PHASE 3 (M4+, derrière le gate) : couche équipe
  [Action / CLI] --report push (token, opt-in, non bloquant)--> [tapelock-server]
  [tapelock-web] -----------------------------------------------> [tapelock-server]
                                                      |-> PostgreSQL
                                                      '-> Object storage (Vault, 3b)
Composants
App	Rôle	Phase	Repo
tapelock (CLI + proxy Go)	Proxy, matching, cassettes, assertions, drift, rapport	1 → 2	OSS
tapelock-action	Installe le binaire épinglé, lance les lanes, commente la PR	2	OSS
helpers/zod (npm)	Zod → JSON Schema	2	OSS
tapelock-server	Ingestion de rapports, historique, multi-tenant, abonnements	3	Privé
tapelock-web	Historique de drift, baselines, équipe, dépenses	3	Privé
Frontière open-core : le CLI ne référence jamais le SaaS au-delà d'un client report push optionnel (timeout court, jamais bloquant). Serveur et dashboard vivent dans un repo privé séparé.

Ce qui change par rapport au plan d'origine
Point du plan	Décision	Raison
Le CLI exécute les assertions dès S1	Assertions en Layer 2 (M1)	Cohérence avec la feuille de route ; S1 = capture/replay fidèles
Assertions « JSON Schema / Zod » dans le CLI	JSON Schema seul + helper Zod	Go n'exécute pas Zod
Webhook GitHub côté serveur pour commenter les PR	L'Action commente elle-même	Un serveur en moins, surface d'attaque réduite
Backend + dashboard + paiement au Mois 2	Phase 3, M4+ derrière un gate	Ne pas bâtir l'infra avant d'avoir un signal payant (§4)
Keycloak / Supabase Auth	GitHub OAuth (ta cible vit sur GitHub) + tokens API par projet	Keycloak = charge d'exploitation élevée pour un solo
Serveur en Go « pour la cohérence »	Choisir selon ta vélocité (NestJS/Nuxt vs Go)	La cohérence de langage compte moins que la vitesse ; le contrat est un JSON Schema/OpenAPI partagé, pas le langage
Object storage dès Phase 2	Différé (3b, Vault)	Les cassettes contiennent des prompts réels = PII et exigences sécurité
« Le client paie pour le dashboard »	Il paie pour l'alerte de drift + l'historique + la gouvernance ; le dashboard est le véhicule	La valeur est la détection, pas l'écran
Phase 3 : modèle de données (métadonnées uniquement)
organizations, users, memberships
projects (repo, provider(s))
api_tokens (préfixe visible, hash du token, scope projet, révocable)
runs (repo, sha, branche, lane, verdict, durée, tokens agrégés)
run_results (suite, assertion, verdict, stats)
baselines (suite, fingerprint, acceptée par, date)
drift_events (cause : modèle changé / comportement changé)
Règles d'ingestion et de sécurité (OWASP)

Tenant isolé par tenant_id sur chaque table + Row-Level Security PostgreSQL.
Tokens à haute entropie, stockés hachés, scopés par projet, révocables ; rate limiting.
Ingestion idempotente sur (project, run_id) ; schéma versionné (OpenAPI).
report push : timeout ~3 s, échec ignoré, jamais bloquant.
3a : rapports uniquement. 3b (Vault) : payloads chiffrés, préfixe et clé par tenant, redaction serveur, rétention configurable.
Facturation : Merchant of Record (Lemon Squeezy / Paddle) ; vérifier le support des paiements sortants vers le Cameroun avant de s'engager.
2.1 Structure du repo Go (OSS)
Le découpage suit pur / I/O, pas des couches abstraites : les tests du hasher ne doivent jamais dépendre du réseau.

tapelock/
├─ cmd/tapelock/            # main, cobra, chargement de config, exit codes
├─ internal/
│  ├─ core/                  # PUR, zéro I/O
│  │  ├─ canon/  match/      # JCS, normalizers, KeyBuilder
│  │  ├─ cassette/           # codec + validation de schéma
│  │  ├─ provider/           # port + adapters purs (openai/, anthropic/) : bytes -> struct
│  │  ├─ assertion/  drift/  # verdicts, fingerprint, diff
│  ├─ proxy/                 # I/O : serveur, tee SSE, replayer, client upstream
│  ├─ store/                 # fichiers cassettes (écriture atomique)
│  └─ report/                # renderers md, json, junit
├─ helpers/zod/              # package npm
├─ action/                   # GitHub Action (composite)
├─ testdata/                 # cassettes golden, vecteurs RFC 8785
└─ docs/
Règle : core n'importe ni proxy ni store. Les ports (CassetteStore, Upstream, Clock) sont définis dans core, implémentés à l'extérieur.
Pas de pkg/ public en v0 : aucune API figée tant que le format de cassette n'est pas stable.
✅ logique testable en TDD sans réseau (table-driven + fuzz du canonicalizer) / ⚠️ un package de plus qu'une structure à 2 couches.
2.2 Configuration : tapelock.yaml
Parser YAML 1.2 strict : clés inconnues = erreur (exit 2), pour éviter les typos silencieux dans les seuils.

version: 1

providers:
  openai: { upstream: https://api.openai.com }
  # anthropic: Layer 2

cassettes:
  dir: .tapelock/cassettes

match:
  strategy: strict            # strict | relaxed (model + messages + tools)
  ignore: ["$.user", "$.metadata"]
  normalizers:
    - uuid
    - iso8601
    - { path: "$.messages[*].content", regex: "ORD-\\d+", replace: "<order>" }

replay:
  strict: false
  stream_timing: instant      # instant | recorded | scaled:0.2

redact:
  headers: [authorization, x-api-key, openai-organization]

suites:                        # Layer 2
  - name: extract-invoice
    select: { model: "gpt-*", tag: invoice }   # tag via header x-tapelock-tag (retiré avant l'upstream)
    assert:
      schema: schemas/invoice.json
      finish_reason: [stop]
      tool_calls: { allow: [lookup_customer], args_schema: schemas/lookup.json }
      latency: { max_ttfb_ms: 1500, max_total_ms: 8000 }
      usage: { max_total_tokens: 2000, on_missing: unknown }

drift:                         # Layer 2
  samples: 5
  budget_usd: 2.00
  fingerprint: [json_shape, tool_names, finish_reason, usage_bucket]
  tolerance:
    latency: { rel: 0.30, abs_floor_ms: 300 }
    fail_rate_margin: 0.10
2.3 Cassette : JSONL typé, un fichier par entrée
Un fichier .jsonl par entrée (nommé par préfixe de clé). Écriture atomique (temporaire + rename). Footer absent = entrée tronquée = rejetée.

{"t":"header","v":1,"key":"sha256:…","occ":0,"provider":"openai","endpoint":"/v1/chat/completions","recorded_at":"2026-09-19T10:00:00Z","request":{"body":{},"normalized":["$.messages[1].content"]},"response":{"status":200,"stream":true,"headers":{"content-type":"text/event-stream"}}}
{"t":"event","i":0,"dt_ms":0,"raw":"data: {…}\n\n"}
{"t":"event","i":1,"dt_ms":38,"raw":"data: {…}\n\n"}
{"t":"footer","events":42,"ttfb_ms":412,"total_ms":3120,"usage":{"in":812,"out":204},"finish_reason":"stop","model_served":"…","complete":true}
Non-stream : une ligne {"t":"body","raw":"…"} à la place des events.
usage peut valoir null si le stream n'en contenait pas (voir 1.1).
Schéma strict : additionalProperties: false, v obligatoire, version inconnue = exit 5.
raw = frame SSE brute ; les vues parsées (tool calls reconstitués, fingerprint) sont calculées à la demande par le reducer du provider.
Bloc baseline (Layer 2) dans le header : fingerprint + nombre d'échantillons.
✅ diffable, sans conflits de merge / ⚠️ nombreux petits fichiers (dédup par clé + cassette prune).
OWASP : les cassettes contiennent des prompts réels. Redaction des headers d'auth par défaut ; scan de secrets (regex de clés API connues) avant écriture.
2.4 Intégration CI (GitHub Actions)
PR lane : replay strict, gratuite

name: tapelock-pr
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  replay:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<sha>
      - uses: <org>/tapelock-action@<sha>   # binaire épinglé + checksum
        with:
          version: 0.1.0
          run: npm test
          strict: true
          comment: true
Drift lane : live, secrets isolés

name: tapelock-drift
on:
  schedule: [{ cron: "0 3 * * *" }]
  workflow_dispatch:
permissions:
  contents: read
  issues: write
jobs:
  drift:
    runs-on: ubuntu-latest
    environment: llm-drift            # secrets scopés
    steps:
      - uses: actions/checkout@<sha>
      - uses: <org>/tapelock-action@<sha>
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
        with:
          command: drift run --samples 5 --budget-usd 2
          on-drift: issue
Sécurité CI : actions épinglées par SHA, permissions minimales, secrets uniquement sur schedule/workflow_dispatch, jamais pull_request_target avec checkout du code de la PR.

3. Faux positifs et non-déterminisme
Constat : la PR lane est déterministe par construction (replay). Tout le risque de faux positifs est dans la drift lane.

3.1 Tiers d'assertions
Tier	Assertions	Politique
0 : rigueur absolue	schema, noms de tool calls, finish_reason en allowlist	PR lane : échec dur. Drift lane : échec si le taux d'échec dépasse baseline + marge
1 : statistique	latence (TTFB), tokens	Jamais sur un échantillon unique. Médiane/p95 vs baseline, tolérance relative + plancher absolu
2 : informatif	nouveaux champs optionnels, variation d'usage mineure, coût USD	Warning, ne fait jamais échouer
3.2 Mécanismes anti-flake
Comparer des taux, pas des occurrences : un schéma valide à 97 % est normal. Drift = fail_rate > baseline + margin, avec N ≥ 5.
Confirmation avant alerte : un échec suspect est ré-échantillonné ; il n'escalade que s'il se reproduit, sinon classé flaky avec son taux.
Latence : tolérance relative et plancher absolu ; cold start écarté.
Tolérance additive : champ nouveau optionnel = info ; champ supprimé ou type modifié = échec Tier 0.
Quarantaine avec expiration : tapelock quarantine add --reason … --until 14d ; l'assertion tourne et est rapportée sans bloquer. TTL pour éviter la dette permanente.
Baseline explicite : jamais mise à jour automatiquement, versionnée en git.
Attribution de cause via model_served.
Runs live : temperature: 0 et seed si supportés (aucun provider ne garantit le déterminisme total).
3.3 Biais à surveiller
Échantillonnage : baseline sur 1 sortie chanceuse → N ≥ 5.
Horaire : latence en heures de pointe → médiane, jamais un échantillon.
Dérive de baseline : acceptée trop souvent, elle masque une dégradation lente → historique des baseline accept visible dans le rapport.
Trade-off central : rigueur (Tier 0) vs confiance des utilisateurs. Une seule alerte inutile en CI et l'équipe désactive l'outil : préférer un faux négatif tolérable à un faux positif récurrent.

4. Plan révisé et gate de validation
Phase	Contenu	Infra
S1	CLI Go : record, replay, --strict, JCS + normalizers, proxy SSE, OpenAI uniquement	Aucune
M1-2	Assertions, Anthropic, drift, tapelock-action, rapport PR sticky	Aucune (GitHub uniquement)
M2-4	3-5 design partners sur la drift lane ; pré-vente à prix fondateur	Aucune
Gate M4	Au moins un partenaire prêt à payer ou à s'engager sur historique/alertes d'équipe ?	:
M4-8	tapelock-server (3a, rapports) + tapelock-web + facturation MoR ; Vault (3b) plus tard	Postgres, hébergement
✅ pas d'infra ni de dette d'exploitation avant un signal payant / ⚠️ pas de monétisation avant M4+ (acceptable : le cash n'est pas urgent).
Gate négatif : aucun engagement à M4 → l'hypothèse « les équipes paient pour la surveillance de drift » est fausse ; pivoter avant d'écrire le SaaS.
Plan S1 (TDD)

J1 : matrice de tests (ordre des clés, flottants, Unicode, normalizers avant JCS, occurrences, champs exclus) puis canonicalizer + KeyBuilder, fuzz.
J2 : format de cassette, store atomique, parsing strict.
J3 : proxy passthrough SSE (test avec le SDK OpenAI Node).
J4 : replay + timing + occurrences.
J5 : wrapper -- <cmd>, --strict, exit codes, --explain-miss.
J6 : redaction, release (goreleaser, npm avec binaires par plateforme).
J7 : README, GIF de démo, compteur « tokens économisés ».
5. Décisions ouvertes
Nom : checklist de disponibilité de Tapelock (§0.1) avant tout achat de domaine ou publication.
Stack du serveur (Phase 3) : Go vs NestJS/Nuxt, selon la vélocité.
Stockage des cassettes : git vs artefact/cache CI (impacte le futur Vault).
Licence : Apache-2.0 (adoption maximale) vs BSL/AGPL (protection contre un clone hébergé). Le moat réel reste les baselines et le workflow.
Facturation : MoR vs entité étrangère ; vérifier les paiements sortants vers le Cameroun.
Critères chiffrés du gate M4 (nombre de partenaires, seuil de prix).