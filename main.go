package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"text/template"
	"unicode"

	pg_query "github.com/lfittl/pg_query_go"
	nodes "github.com/lfittl/pg_query_go/nodes"
)

const gencodeTmpl = `// Code generated by pgsqlconsts; DO NOT EDIT.
package {{.Package}}

{{range .Tables}}
// {{GoTitle .Name}} contains constants for the {{.Name}} table
var {{GoTitle .Name}} = struct{
	TableName string
	{{- range .Columns}}
	{{GoTitle .Name}} string // {{.Type}}
	{{- end}}
}{"{{.Name}}",{{range .Columns}}"{{.Name}}",{{end}} }
{{end}}
`

type Table struct {
	Name    string
	Columns []Column
}
type Column struct {
	Name string
	Type string
}

type Data struct {
	Package string
	Tables  []Table
}

func main() {
	pkg := flag.String("package", "models", "package name")
	matchTables := flag.String("tables", "", "comma separated list of tables to generate (default all tables)")
	outputFile := flag.String("output", "", "if specified, file to write generated code to (default stdout)")
	templateFile := flag.String("template", "", "template file to use for generation")

	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [OPTION]... [SQLFILE]\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	sqlFile := flag.Arg(0)

	// go read our SQL
	fc, err := ioutil.ReadFile(sqlFile)
	if err != nil {
		log.Fatalf("unable to open %s: %s", sqlFile, err)
	}
	stmt, err := pg_query.Parse(string(fc))
	if err != nil {
		log.Fatalf("error parsing sql: %s", err)
	}

	// walk it, looking for "CREATE TABLE" statements
	createTables := []nodes.CreateStmt{}
	for _, n := range stmt.Statements {
		switch n := n.(type) {
		case nodes.RawStmt:
			switch s := n.Stmt.(type) {
			case nodes.CreateStmt:
				createTables = append(createTables, s)
			}
		case nodes.CreateStmt:
			createTables = append(createTables, n)
		default:
			log.Printf("unexpected statement type %T\n", n)
		}
	}

	tables := map[string]bool{}
	if *matchTables != "" {
		for _, table := range strings.Split(*matchTables, ",") {
			tables[table] = true
		}
	}

	// build up our template data structure
	data := Data{
		Package: *pkg,
	}
	for _, s := range createTables {
		tableName := *s.Relation.Relname
		if len(tables) > 0 && !tables[tableName] {
			continue
		}

		tbl := Table{
			Name: tableName,
		}
		for _, col := range s.TableElts.Items {
			cd, ok := col.(nodes.ColumnDef)
			if !ok {
				continue
			}
			tbl.Columns = append(tbl.Columns,
				Column{
					Name: *cd.Colname,
					Type: toString(cd.TypeName.Names.Items),
				})

		}
		data.Tables = append(data.Tables, tbl)
	}

	fmap := template.FuncMap{}
	fmap["Title"] = strings.Title
	fmap["GoTitle"] = goTitleCase
	fmap["ToUpper"] = strings.ToUpper
	fmap["ToLower"] = strings.ToLower

	templateText := gencodeTmpl
	if *templateFile != "" {
		d, err := ioutil.ReadFile(*templateFile)
		if err != nil {
			log.Fatalf("unable to read template file: %s", err)
		}
		templateText = string(d)
	}

	tmpl, err := template.New("").Funcs(fmap).Parse(templateText)
	if err != nil {
		log.Fatalf("unable to parse template: %s", err)
	}
	buf := bytes.Buffer{}
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Fatalf("error executing template: %s", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		io.Copy(os.Stderr, &buf)
		log.Fatalf("generated bad code: %s", err)
	}

	var w io.Writer = os.Stdout
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			log.Fatalf("error creating output file: %s", err)
		}
		w = f
		defer f.Close()
	}
	io.Copy(w, bytes.NewReader(formatted))
}

func goTitleCase(s string) string {
	switch s {
	case "id":
		return "ID"
	}
	buf := bytes.Buffer{}
	titlecaseNext := false

	for i, c := range s {
		if i == 0 || titlecaseNext {
			c = unicode.ToTitle(c)
			titlecaseNext = false
		}
		switch c {
		case ' ':
			titlecaseNext = true
		case '_':
			titlecaseNext = true
			continue
		}
		buf.WriteRune(c)
	}

	return buf.String()
}

func toString(nod []nodes.Node) string {
	buf := bytes.Buffer{}
	for _, n := range nod {
		switch n := n.(type) {
		case nodes.String:
			if buf.Len() != 0 {
				buf.WriteByte(' ')
			}
			buf.WriteString(n.Str)
		default:
			log.Printf("unhandled node type: %T", n)
		}
	}
	return buf.String()
}
