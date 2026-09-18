package accountsmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Column struct {
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Nullable      bool    `json:"nullable"`
	Default       *string `json:"default"`
	Charset       string  `json:"charset,omitempty"`
	Collation     string  `json:"collation,omitempty"`
	AutoIncrement bool    `json:"auto_increment,omitempty"`
	OnUpdate      string  `json:"on_update,omitempty"`
}
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}
type Check struct{ Name, Expression string }
type Step struct {
	ID       string   `json:"id"`
	Checksum string   `json:"checksum"`
	Table    string   `json:"table"`
	SQL      string   `json:"-"`
	Create   bool     `json:"-"`
	Columns  []Column `json:"-"`
	Indexes  []Index  `json:"-"`
	Checks   []Check  `json:"-"`
}

var identifier = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")
var createPattern = regexp.MustCompile("(?is)^CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([a-zA-Z_][a-zA-Z0-9_]*)`?\\s*\\((.*)\\)\\s*(?:ENGINE\\s*=.*)?$")
var alterPattern = regexp.MustCompile("(?is)^ALTER\\s+TABLE\\s+`?([a-zA-Z_][a-zA-Z0-9_]*)`?\\s+(.+)$")
var columnPattern = regexp.MustCompile("(?is)^`?([a-zA-Z_][a-zA-Z0-9_]*)`?\\s+((?:BIGINT|TINYINT|SMALLINT|INTEGER|INT|BOOLEAN|BOOL|DATETIME|TIMESTAMP|VARCHAR|CHAR|BINARY|TEXT|LONGTEXT|MEDIUMBLOB)(?:\\([0-9]+\\))?(?:\\s+UNSIGNED)?)(.*)$")
var indexPattern = regexp.MustCompile("(?is)^(PRIMARY\\s+KEY|(?:UNIQUE\\s+)?(?:KEY|INDEX)\\s+`?[a-zA-Z_][a-zA-Z0-9_]*`?)\\s*\\(([^)]+)\\)$")
var checkPattern = regexp.MustCompile("(?is)^CONSTRAINT\\s+`?([a-zA-Z_][a-zA-Z0-9_]*)`?\\s+CHECK\\s*\\((.+)\\)$")
var commentPattern = regexp.MustCompile("(?is)\\s+COMMENT\\s+'(?:''|[^'])*'")
var defaultPattern = regexp.MustCompile("(?is)\\bDEFAULT\\s+('(?:''|[^'])*'|CURRENT_TIMESTAMP(?:\\([0-9]*\\))?|NULL|TRUE|FALSE|-?[0-9]+)")
var charsetPattern = regexp.MustCompile("(?i)CHARACTER\\s+SET\\s+([a-zA-Z0-9_]+)")
var collationPattern = regexp.MustCompile("(?i)COLLATE\\s+([a-zA-Z0-9_]+)")
var onUpdatePattern = regexp.MustCompile("(?i)ON\\s+UPDATE\\s+(CURRENT_TIMESTAMP(?:\\([0-9]*\\))?)")
var intWidthPattern = regexp.MustCompile("(?i)^(bigint|tinyint|smallint|int|integer)\\([0-9]+\\)")

// ParseDDL only accepts additive table/column/index changes from embedded files.
// Every ALTER clause gets its own durable completion marker because DDL commits implicitly.
func ParseDDL(name string, raw []byte) ([]Step, error) {
	var clean []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		clean = append(clean, line)
	}
	statements, err := splitSQL(strings.Join(clean, "\n"), ';')
	if err != nil {
		return nil, err
	}
	var steps []Step
	for _, statement := range statements {
		if match := createPattern.FindStringSubmatch(statement); match != nil {
			definitions, err := splitSQL(match[2], ',')
			if err != nil {
				return nil, err
			}
			step := Step{Table: match[1], SQL: statement, Create: true}
			for _, definition := range definitions {
				if err := parseDefinition(&step, definition); err != nil {
					return nil, err
				}
			}
			steps = append(steps, step)
		} else if match := alterPattern.FindStringSubmatch(statement); match != nil {
			clauses, err := splitSQL(match[2], ',')
			if err != nil {
				return nil, err
			}
			for _, clause := range clauses {
				if !strings.HasPrefix(strings.ToUpper(clause), "ADD ") {
					return nil, errors.New("only additive ALTER clauses are supported")
				}
				definition := strings.TrimSpace(clause[4:])
				if strings.HasPrefix(strings.ToUpper(definition), "COLUMN ") {
					definition = strings.TrimSpace(definition[7:])
				}
				step := Step{Table: match[1], SQL: "ALTER TABLE `" + match[1] + "` ADD " + definition}
				if err := parseDefinition(&step, definition); err != nil {
					return nil, err
				}
				steps = append(steps, step)
			}
		} else {
			return nil, errors.New("unsupported migration SQL statement")
		}
	}
	for i := range steps {
		steps[i].ID = fmt.Sprintf("%s:%03d", name, i+1)
		sum := sha256.Sum256([]byte(steps[i].SQL))
		steps[i].Checksum = hex.EncodeToString(sum[:])
	}
	return steps, nil
}

// splitSQL understands quotes and parenthesized expressions; delimiters in defaults are data.
func splitSQL(input string, delimiter rune) ([]string, error) {
	var output []string
	var quote rune
	depth, start := 0, 0
	chars := []rune(input)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(chars) && chars[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
		}
		if depth < 0 {
			return nil, errors.New("unbalanced DDL parentheses")
		}
		if c == delimiter && depth == 0 {
			if part := strings.TrimSpace(string(chars[start:i])); part != "" {
				output = append(output, part)
			}
			start = i + 1
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("unbalanced DDL quotes or parentheses")
	}
	if part := strings.TrimSpace(string(chars[start:])); part != "" {
		output = append(output, part)
	}
	return output, nil
}

// parseDefinition 将单个列、索引或 CHECK 定义解析为可核验的结构，规范化属性并拒绝不支持或未解析的内容。
func parseDefinition(step *Step, definition string) error {
	definition = strings.TrimSpace(commentPattern.ReplaceAllString(definition, ""))
	if match := checkPattern.FindStringSubmatch(definition); match != nil {
		step.Checks = append(step.Checks, Check{Name: match[1], Expression: normalizeExpression(match[2])})
		return nil
	}
	if match := indexPattern.FindStringSubmatch(definition); match != nil {
		words := strings.Fields(strings.ReplaceAll(match[1], "`", ""))
		index := Index{Name: words[len(words)-1], Unique: strings.HasPrefix(strings.ToUpper(words[0]), "UNIQUE")}
		if strings.EqualFold(words[0], "PRIMARY") {
			index.Name = "PRIMARY"
			index.Unique = true
		}
		for _, part := range strings.Split(match[2], ",") {
			column := strings.Trim(strings.TrimSpace(part), "`")
			if !identifier.MatchString(column) {
				return errors.New("unsupported index expression")
			}
			index.Columns = append(index.Columns, column)
		}
		step.Indexes = append(step.Indexes, index)
		return nil
	}
	match := columnPattern.FindStringSubmatch(definition)
	if match == nil {
		return errors.New("unsupported migration column definition")
	}
	tail := match[3]
	upper := strings.ToUpper(tail)
	column := Column{Name: match[1], Type: normalizeType(match[2]), Nullable: !strings.Contains(upper, "NOT NULL") && !strings.Contains(upper, "PRIMARY KEY"), AutoIncrement: strings.Contains(upper, "AUTO_INCREMENT")}
	if value := defaultPattern.FindStringSubmatch(tail); value != nil {
		column.Default = normalizeDefault(value[1])
	}
	if value := charsetPattern.FindStringSubmatch(tail); value != nil {
		column.Charset = strings.ToLower(value[1])
	}
	if value := collationPattern.FindStringSubmatch(tail); value != nil {
		column.Collation = strings.ToLower(value[1])
	}
	if value := onUpdatePattern.FindStringSubmatch(tail); value != nil {
		column.OnUpdate = normalizeExpression(value[1])
	}
	if strings.Contains(upper, "PRIMARY KEY") {
		step.Indexes = append(step.Indexes, Index{Name: "PRIMARY", Unique: true, Columns: []string{column.Name}})
	}
	// Column-level CHECKs are also verified via INFORMATION_SCHEMA, by expression.
	if pos := strings.Index(upper, "CHECK"); pos >= 0 {
		expr := strings.TrimSpace(tail[pos+5:])
		step.Checks = append(step.Checks, Check{Expression: normalizeExpression(expr)})
		tail = tail[:pos]
	}
	remainder := commentPattern.ReplaceAllString(tail, "")
	for _, pattern := range []*regexp.Regexp{defaultPattern, charsetPattern, collationPattern, onUpdatePattern} {
		remainder = pattern.ReplaceAllString(remainder, "")
	}
	for _, word := range []string{"NOT NULL", "NULL", "AUTO_INCREMENT", "PRIMARY KEY"} {
		remainder = strings.ReplaceAll(strings.ToUpper(remainder), word, "")
	}
	if strings.TrimSpace(remainder) != "" {
		return errors.New("unparsed migration column attributes")
	}
	step.Columns = append(step.Columns, column)
	return nil
}

func normalizeType(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	value = intWidthPattern.ReplaceAllString(value, "$1")
	if value == "bool" || value == "boolean" {
		return "tinyint"
	}
	if strings.HasPrefix(value, "integer") {
		value = "int" + value[len("integer"):]
	}
	return value
}
func normalizeDefault(value string) *string {
	if strings.EqualFold(value, "NULL") {
		return nil
	}
	if strings.EqualFold(value, "FALSE") {
		value = "0"
	}
	if strings.EqualFold(value, "TRUE") {
		value = "1"
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	} else {
		value = normalizeExpression(value)
	}
	return &value
}
func normalizeExpression(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "current_timestamp()", "current_timestamp")
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = value[1 : len(value)-1]
	}
	return value
}
