package sessions

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxSearchQuery   = 128
	maxFilterValue   = 128
	likeEscapeChar   = `\`
	failoverDecision = "hop"
)

func sqlCol(alias, column string) string {
	if alias == "" {
		return column
	}
	return alias + "." + column
}

func sqlTyped(alias, column, kind string, required bool) string {
	col := sqlCol(alias, column)
	if required {
		if kind == "text" {
			return fmt.Sprintf("typeof(%s) = 'text' AND %s != ''", col, col)
		}
		return fmt.Sprintf("typeof(%s) = '%s'", col, kind)
	}
	return fmt.Sprintf("(%s IS NULL OR typeof(%s) = '%s')", col, col, kind)
}

func sqlTrustedRequest(alias string) string {
	cost := sqlCol(alias, "cost")
	return strings.Join([]string{
		sqlTyped(alias, "id", "text", true),
		sqlTyped(alias, "session_id", "text", true),
		sqlTyped(alias, "started_at", "integer", true),
		sqlTyped(alias, "protocol", "text", true),
		sqlTyped(alias, "method", "text", true),
		sqlTyped(alias, "path", "text", true),
		sqlTyped(alias, "correlation_id", "text", false),
		sqlTyped(alias, "status", "integer", false),
		sqlTyped(alias, "duration_ms", "integer", false),
		sqlTyped(alias, "request_bytes", "integer", false),
		sqlTyped(alias, "response_bytes", "integer", false),
		sqlTyped(alias, "route_kind", "text", false),
		sqlTyped(alias, "requested_model", "text", false),
		sqlTyped(alias, "requested_provider", "text", false),
		sqlTyped(alias, "resolved_model", "text", false),
		sqlTyped(alias, "provider", "text", false),
		sqlTyped(alias, "combo_id", "text", false),
		sqlTyped(alias, "policy_id", "text", false),
		sqlTyped(alias, "committed_member", "text", false),
		sqlTyped(alias, "attempts_json", "text", false),
		sqlTyped(alias, "input_tokens", "integer", false),
		sqlTyped(alias, "cached_input_tokens", "integer", false),
		sqlTyped(alias, "output_tokens", "integer", false),
		sqlTyped(alias, "total_tokens", "integer", false),
		fmt.Sprintf("(%s IS NULL OR typeof(%s) IN ('real', 'integer'))", cost, cost),
		sqlTyped(alias, "currency", "text", false),
	}, " AND ")
}

func sqlKnownProtocol(alias string) string {
	col := sqlCol(alias, "protocol")
	return fmt.Sprintf("%s IN ('%s', '%s', '%s')", col, ProtocolResponses, ProtocolChat, ProtocolAnthropic)
}

func sqlQualifiedCost(alias string) string {
	cost := sqlCol(alias, "cost")
	currency := sqlCol(alias, "currency")
	return fmt.Sprintf("(%s) AND typeof(%s) IN ('real', 'integer') AND typeof(%s) = 'text' AND trim(%s) != ''",
		sqlTrustedRequest(alias), cost, currency, currency)
}

func sqlHasFailoverHop(alias string) string {
	col := sqlCol(alias, "attempts_json")
	elem := `'$[' || hop.key || ']'`
	return fmt.Sprintf(`json_valid(%s) AND json_type(%s) = 'array' AND EXISTS (
    SELECT 1 FROM json_each(%s) AS hop
    WHERE json_type(%s, %s) = 'object'
      AND json_extract(%s, %s || '.decision') = ?
  )`, col, col, col, col, elem, col, elem)
}

func (s *Store) ensureQueryIndexes() error {
	_, err := s.db.Exec(`
CREATE INDEX IF NOT EXISTS sessions_namespace
  ON sessions(namespace);
CREATE INDEX IF NOT EXISTS requests_session_protocol
  ON requests(session_id, protocol);
CREATE INDEX IF NOT EXISTS requests_session_provider
  ON requests(session_id, provider);
CREATE INDEX IF NOT EXISTS requests_session_resolved_model
  ON requests(session_id, resolved_model);
CREATE INDEX IF NOT EXISTS requests_session_policy
  ON requests(session_id, policy_id);
CREATE INDEX IF NOT EXISTS requests_session_combo
  ON requests(session_id, combo_id);
`)
	return err
}

func validateListOptions(opts ListOptions) error {
	if _, err := normalizedSearch(opts.Q); err != nil {
		return err
	}
	if err := validateOptionalFilter("namespace", opts.Namespace); err != nil {
		return err
	}
	if protocol := strings.TrimSpace(opts.Protocol); protocol != "" {
		if !knownProtocol(protocol) {
			return fmt.Errorf("%w: protocol", ErrInvalidFilter)
		}
	}
	for _, item := range [][2]string{
		{"provider", opts.Provider},
		{"model", opts.Model},
		{"policy", opts.Policy},
		{"combo", opts.Combo},
	} {
		if err := validateOptionalFilter(item[0], item[1]); err != nil {
			return err
		}
	}
	return nil
}

func knownProtocol(value string) bool {
	switch strings.TrimSpace(value) {
	case ProtocolResponses, ProtocolChat, ProtocolAnthropic:
		return true
	default:
		return false
	}
}

func normalizedSearch(raw string) (string, error) {
	q := strings.TrimSpace(raw)
	if q == "" {
		return "", nil
	}
	if utf8.RuneCountInString(q) > maxSearchQuery {
		return "", ErrInvalidQuery
	}
	return q, nil
}

func validateOptionalFilter(name, raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > maxFilterValue || strings.ContainsAny(value, "\x00") {
		return fmt.Errorf("%w: %s", ErrInvalidFilter, name)
	}
	return nil
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(
		likeEscapeChar, likeEscapeChar+likeEscapeChar,
		"%", likeEscapeChar+"%",
		"_", likeEscapeChar+"_",
	)
	return replacer.Replace(value)
}

func (s *Store) appendListFilters(query string, args []any, opts ListOptions, cutoff int64) (string, []any, error) {
	if err := validateListOptions(opts); err != nil {
		return "", nil, err
	}
	timeOK := fmt.Sprintf(sqlIntegerTime, "last_activity_at")
	query += ` WHERE ` + timeOK + ` AND last_activity_at >= ?`
	args = append(args, cutoff)
	if q, err := normalizedSearch(opts.Q); err != nil {
		return "", nil, err
	} else if q != "" {
		pattern := "%" + escapeLike(q) + "%"
		query += ` AND (id LIKE ? ESCAPE '\' COLLATE NOCASE OR namespace LIKE ? ESCAPE '\' COLLATE NOCASE OR external_id LIKE ? ESCAPE '\' COLLATE NOCASE)`
		args = append(args, pattern, pattern, pattern)
	}
	if ns := strings.TrimSpace(opts.Namespace); ns != "" {
		query += ` AND namespace = ?`
		args = append(args, ns)
	}
	query, args = appendRequestExists(query, args, "protocol", strings.TrimSpace(opts.Protocol))
	query, args = appendRequestExists(query, args, "provider", strings.TrimSpace(opts.Provider))
	query, args = appendRequestExists(query, args, "resolved_model", strings.TrimSpace(opts.Model))
	query, args = appendRequestExists(query, args, "policy_id", strings.TrimSpace(opts.Policy))
	query, args = appendRequestExists(query, args, "combo_id", strings.TrimSpace(opts.Combo))
	return query, args, nil
}

func appendRequestExists(query string, args []any, column, value string) (string, []any) {
	if value == "" {
		return query, args
	}
	query += ` AND EXISTS (SELECT 1 FROM requests r WHERE r.session_id = sessions.id AND ` + sqlTrustedRequest("r") + ` AND typeof(r.` + column + `) = 'text' AND r.` + column + ` != '' AND r.` + column + ` = ?)`
	args = append(args, value)
	return query, args
}

func (s *Store) attachListProtocols(rows []Summary) error {
	for i := range rows {
		rows[i].Protocols = []string{}
	}
	if len(rows) == 0 {
		return nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, len(rows))
	index := make(map[string]int, len(rows))
	for i, row := range rows {
		placeholders[i] = "?"
		args[i] = row.ID
		index[row.ID] = i
	}
	query := `SELECT r.session_id, r.protocol FROM requests r WHERE r.session_id IN (` + strings.Join(placeholders, ",") + `) AND ` + sqlTrustedRequest("r") + ` AND ` + sqlKnownProtocol("r") + ` GROUP BY r.session_id, r.protocol ORDER BY r.protocol COLLATE NOCASE, r.protocol`
	result, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer result.Close()
	for result.Next() {
		var sessionID, protocol string
		if err := result.Scan(&sessionID, &protocol); err != nil {
			return err
		}
		i, ok := index[sessionID]
		if !ok {
			continue
		}
		rows[i].Protocols = append(rows[i].Protocols, protocol)
	}
	return result.Err()
}

func (s *Store) loadAggregates(sessionID string) (Aggregates, error) {
	out := Aggregates{
		Protocols: []string{},
		Models:    []string{},
		Providers: []string{},
		PolicyIDs: []string{},
		ComboIDs:  []string{},
	}
	trusted := sqlTrustedRequest("")
	qualifiedCost := sqlQualifiedCost("")
	var (
		total                                     int
		inputN, cachedN, outputN, tokensN, costN  sql.NullInt64
		inputSum, cachedSum, outputSum, tokensSum sql.NullInt64
		costSum                                   sql.NullFloat64
		failoverN                                 sql.NullInt64
	)
	err := s.db.QueryRow(`
SELECT
  COUNT(*),
  SUM(CASE WHEN (`+trusted+`) AND typeof(input_tokens) = 'integer' THEN 1 ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(input_tokens) = 'integer' THEN input_tokens ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(cached_input_tokens) = 'integer' THEN 1 ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(cached_input_tokens) = 'integer' THEN cached_input_tokens ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(output_tokens) = 'integer' THEN 1 ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(output_tokens) = 'integer' THEN output_tokens ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(total_tokens) = 'integer' THEN 1 ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND typeof(total_tokens) = 'integer' THEN total_tokens ELSE 0 END),
  SUM(CASE WHEN (`+qualifiedCost+`) THEN 1 ELSE 0 END),
  SUM(CASE WHEN (`+qualifiedCost+`) THEN cost ELSE 0 END),
  SUM(CASE WHEN (`+trusted+`) AND `+sqlHasFailoverHop("")+` THEN 1 ELSE 0 END)
FROM requests
WHERE session_id = ?`, failoverDecision, sessionID).Scan(
		&total, &inputN, &inputSum, &cachedN, &cachedSum, &outputN, &outputSum, &tokensN, &tokensSum, &costN, &costSum, &failoverN,
	)
	if err != nil {
		return Aggregates{}, err
	}
	out.Usage.InputTokens = intCoverage(inputSum, inputN, total)
	out.Usage.CachedInputTokens = intCoverage(cachedSum, cachedN, total)
	out.Usage.OutputTokens = intCoverage(outputSum, outputN, total)
	out.Usage.TotalTokens = intCoverage(tokensSum, tokensN, total)
	currencies, err := s.distinctTexts(sessionID, qualifiedCost, "currency")
	if err != nil {
		return Aggregates{}, err
	}
	out.Usage.Cost = costCoverage(costSum, costN, total, currencies)
	if protocols, err := s.distinctTexts(sessionID, sqlTrustedRequest("")+` AND `+sqlKnownProtocol(""), "protocol"); err != nil {
		return Aggregates{}, err
	} else {
		out.Protocols = protocols
	}
	if models, err := s.distinctTexts(sessionID, sqlTrustedRequest("")+` AND typeof(resolved_model) = 'text' AND resolved_model != ''`, "resolved_model"); err != nil {
		return Aggregates{}, err
	} else {
		out.Models = models
	}
	if providers, err := s.distinctTexts(sessionID, sqlTrustedRequest("")+` AND typeof(provider) = 'text' AND provider != ''`, "provider"); err != nil {
		return Aggregates{}, err
	} else {
		out.Providers = providers
	}
	if policyIDs, err := s.distinctTexts(sessionID, sqlTrustedRequest("")+` AND typeof(policy_id) = 'text' AND policy_id != ''`, "policy_id"); err != nil {
		return Aggregates{}, err
	} else {
		out.PolicyIDs = policyIDs
	}
	if comboIDs, err := s.distinctTexts(sessionID, sqlTrustedRequest("")+` AND typeof(combo_id) = 'text' AND combo_id != ''`, "combo_id"); err != nil {
		return Aggregates{}, err
	} else {
		out.ComboIDs = comboIDs
	}
	out.FailoverRequestCount = int(failoverN.Int64)
	out.HadFailover = out.FailoverRequestCount > 0
	return out, nil
}

func intCoverage(sum sql.NullInt64, attributed sql.NullInt64, total int) FieldCoverage {
	n := int(attributed.Int64)
	out := FieldCoverage{AttributedRequests: n, TotalRequests: total, Complete: total == n}
	if n > 0 {
		value := sum.Int64
		out.Value = &value
	}
	return out
}

func costCoverage(sum sql.NullFloat64, attributed sql.NullInt64, total int, currencies []string) CostCoverage {
	n := int(attributed.Int64)
	out := CostCoverage{
		AttributedRequests: n,
		TotalRequests:      total,
		Currencies:         currencies,
		Complete:           total == n && (n == 0 || len(currencies) == 1),
	}
	if len(currencies) == 1 {
		out.Currency = currencies[0]
	}
	if n > 0 && len(currencies) == 1 {
		value := sum.Float64
		out.Value = &value
	}
	if len(currencies) > 1 {
		out.Complete = false
		out.Value = nil
		out.Currency = ""
	}
	return out
}

func (s *Store) distinctTexts(sessionID, where, column string) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT `+column+` FROM requests WHERE session_id = ? AND `+where+` ORDER BY `+column+` COLLATE NOCASE, `+column, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) FilterValues() (FilterValues, error) {
	empty := FilterValues{
		Protocols:  []string{},
		Namespaces: []string{},
		Providers:  []string{},
		Models:     []string{},
		PolicyIDs:  []string{},
		ComboIDs:   []string{},
	}
	if s == nil {
		return empty, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return FilterValues{}, ErrUnavailable
	}
	if err := s.pruneAtBound(s.clock(), pruneReadBound); err != nil {
		return FilterValues{}, err
	}
	cutoff := s.clock().Add(-s.retention).UnixMilli()
	namespaces, err := s.distinctSessionColumn("namespace", cutoff)
	if err != nil {
		return FilterValues{}, err
	}
	protocols, err := s.distinctLiveRequestColumn("protocol", cutoff, true)
	if err != nil {
		return FilterValues{}, err
	}
	providers, err := s.distinctLiveRequestColumn("provider", cutoff, false)
	if err != nil {
		return FilterValues{}, err
	}
	models, err := s.distinctLiveRequestColumn("resolved_model", cutoff, false)
	if err != nil {
		return FilterValues{}, err
	}
	policyIDs, err := s.distinctLiveRequestColumn("policy_id", cutoff, false)
	if err != nil {
		return FilterValues{}, err
	}
	comboIDs, err := s.distinctLiveRequestColumn("combo_id", cutoff, false)
	if err != nil {
		return FilterValues{}, err
	}
	return FilterValues{
		Protocols:  protocols,
		Namespaces: namespaces,
		Providers:  providers,
		Models:     models,
		PolicyIDs:  policyIDs,
		ComboIDs:   comboIDs,
	}, nil
}

func (s *Store) distinctSessionColumn(column string, cutoff int64) ([]string, error) {
	timeOK := fmt.Sprintf(sqlIntegerTime, "last_activity_at")
	rows, err := s.db.Query(`SELECT DISTINCT `+column+` FROM sessions WHERE `+timeOK+` AND last_activity_at >= ? AND typeof(`+column+`) = 'text' AND `+column+` != '' ORDER BY `+column+` COLLATE NOCASE, `+column, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) distinctLiveRequestColumn(column string, cutoff int64, knownProtocols bool) ([]string, error) {
	extra := ""
	if knownProtocols {
		extra = ` AND ` + sqlKnownProtocol("r")
	}
	rows, err := s.db.Query(`
SELECT DISTINCT r.`+column+`
FROM requests r
JOIN sessions s ON s.id = r.session_id
WHERE typeof(s.last_activity_at) = 'integer' AND s.last_activity_at >= ?
  AND `+sqlTrustedRequest("r")+`
  AND typeof(r.`+column+`) = 'text' AND r.`+column+` != ''`+extra+`
ORDER BY r.`+column+` COLLATE NOCASE, r.`+column, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func ValidSessionID(id string) bool {
	return validSessionID(id)
}
