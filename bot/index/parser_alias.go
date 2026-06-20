package index

import parserpkg "nekocode/bot/index/parser"

type Parser = parserpkg.Parser

func NewParser() *Parser {
	return parserpkg.NewParser()
}
