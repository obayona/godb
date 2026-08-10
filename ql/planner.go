package ql

import (
	"bytes"

	"github.com/obayona/godb/btree"
	"github.com/obayona/godb/table"
)

// scanBound is a constant lower or upper bound found in a filter predicate.
type scanBound struct {
	value     table.Value
	inclusive bool
}

// columnConstraint contains the simple predicates known for one column.
// Ambiguous predicates are ignored so planning can safely fall back to a less
// selective index or to a full scan.
type columnConstraint struct {
	equal     *table.Value
	lower     *scanBound
	upper     *scanBound
	ambiguous bool
}

// qlScanInit chooses the most selective usable index prefix represented by a
// conjunction of simple column-to-constant comparisons. The complete FILTER
// expression is still evaluated for every candidate row, so planning only
// affects performance. Unsupported expressions safely produce a full scan.
func qlScanInit(req *QLScan, tdef *table.TableDef, sc *table.Scanner) error {
	constraints := map[string]*columnConstraint{}
	collectConstraints(req.Filter, constraints)

	type candidate struct {
		index    []string
		eqValues []table.Value
		rangeCol string
		rangeCon *columnConstraint
		score    int
	}
	best := candidate{}
	for _, index := range tdef.Indexes {
		cur := candidate{index: index}
		for _, col := range index {
			con := constraints[col]
			if con == nil || con.ambiguous {
				break
			}
			if con.equal != nil {
				cur.eqValues = append(cur.eqValues, *con.equal)
				cur.score += 2
				continue
			}
			if con.lower != nil || con.upper != nil {
				cur.rangeCol = col
				cur.rangeCon = con
				cur.score++
			}
			break // an index cannot use columns after its first range or gap
		}
		if cur.score > best.score {
			best = cur
		}
	}

	if best.score == 0 {
		// Empty records encode the entire primary-index range.
		sc.Cmp1, sc.Cmp2 = btree.CMP_GE, btree.CMP_LE
		return nil
	}

	eqCols := best.index[:len(best.eqValues)]
	if best.rangeCon == nil {
		sc.Key1 = table.Record{Cols: eqCols, Vals: best.eqValues}
		sc.Key2 = sc.Key1
		sc.Cmp1, sc.Cmp2 = btree.CMP_GE, btree.CMP_LE
		return nil
	}

	cols := append(append([]string(nil), eqCols...), best.rangeCol)
	if lower := best.rangeCon.lower; lower != nil {
		vals := append(append([]table.Value(nil), best.eqValues...), lower.value)
		sc.Key1 = table.Record{Cols: cols, Vals: vals}
		if lower.inclusive {
			sc.Cmp1 = btree.CMP_GE
		} else {
			sc.Cmp1 = btree.CMP_GT
		}
	}
	if upper := best.rangeCon.upper; upper != nil {
		vals := append(append([]table.Value(nil), best.eqValues...), upper.value)
		sc.Key2 = table.Record{Cols: cols, Vals: vals}
		if upper.inclusive {
			sc.Cmp2 = btree.CMP_LE
		} else {
			sc.Cmp2 = btree.CMP_LT
		}
	}
	if best.rangeCon.lower == nil {
		// A lone upper bound scans backward toward the start of the index.
		sc.Key1, sc.Key2 = sc.Key2, table.Record{}
		sc.Cmp1, sc.Cmp2 = sc.Cmp2, btree.CMP_GE
	} else if best.rangeCon.upper == nil {
		sc.Cmp2 = btree.CMP_LE
	}
	return nil
}

func collectConstraints(node QLNode, out map[string]*columnConstraint) {
	if node.Type == QL_AND {
		collectConstraints(node.Kids[0], out)
		collectConstraints(node.Kids[1], out)
		return
	}
	if len(node.Kids) != 2 {
		return
	}
	op := node.Type
	left, right := node.Kids[0], node.Kids[1]
	if left.Type != QL_SYM && right.Type == QL_SYM {
		left, right = right, left
		op = reverseComparison(op)
	}
	if left.Type != QL_SYM {
		return
	}
	value, ok := constantValue(right)
	if !ok {
		return
	}
	col := string(left.Str)
	con := out[col]
	if con == nil {
		con = &columnConstraint{}
		out[col] = con
	}
	switch op {
	case QL_CMP_EQ:
		if con.equal != nil && !valuesEqual(*con.equal, value) {
			con.ambiguous = true
		} else {
			copy := value
			con.equal = &copy
		}
	case QL_CMP_GE, QL_CMP_GT:
		bound := scanBound{value: value, inclusive: op == QL_CMP_GE}
		if con.lower != nil {
			con.ambiguous = true
		} else {
			con.lower = &bound
		}
	case QL_CMP_LE, QL_CMP_LT:
		bound := scanBound{value: value, inclusive: op == QL_CMP_LE}
		if con.upper != nil {
			con.ambiguous = true
		} else {
			con.upper = &bound
		}
	}
}

func constantValue(node QLNode) (table.Value, bool) {
	ctx := QLEvalContex{}
	qlEval(&ctx, node)
	return ctx.out, ctx.err == nil && (ctx.out.Type == table.TYPE_INT64 || ctx.out.Type == table.TYPE_BYTES)
}

func reverseComparison(op uint32) uint32 {
	switch op {
	case QL_CMP_GE:
		return QL_CMP_LE
	case QL_CMP_GT:
		return QL_CMP_LT
	case QL_CMP_LE:
		return QL_CMP_GE
	case QL_CMP_LT:
		return QL_CMP_GT
	default:
		return op
	}
}

func valuesEqual(a, b table.Value) bool {
	return a.Type == b.Type && a.I64 == b.I64 && bytes.Equal(a.Str, b.Str)
}
