package templates

import (
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"text/template/parse"
)

func validateHTMLReferences(t *htmltemplate.Template) error {
	trees := make(map[string]*parse.Tree)
	for _, associated := range t.Templates() {
		trees[associated.Name()] = associated.Tree
	}
	return validateReferences(trees)
}

func validateTextReferences(t *texttemplate.Template) error {
	trees := make(map[string]*parse.Tree)
	for _, associated := range t.Templates() {
		trees[associated.Name()] = associated.Tree
	}
	return validateReferences(trees)
}

// Check references before caching, while HTML trees have not yet been executed/escaped.
// Do not execute with dummy data: missing runtime values are valid at preload time.
func validateReferences(trees map[string]*parse.Tree) error {
	for name, tree := range trees {
		if tree == nil {
			continue
		}
		if err := validateReferenceList(tree.Root, trees); err != nil {
			return fmt.Errorf("validate template %s: %w", name, err)
		}
	}
	return nil
}

func validateReferenceList(list *parse.ListNode, trees map[string]*parse.Tree) error {
	if list == nil {
		return nil
	}
	for _, node := range list.Nodes {
		var err error
		switch n := node.(type) {
		case *parse.TemplateNode:
			if tree := trees[n.Name]; tree == nil || tree.Root == nil {
				return fmt.Errorf("undefined template %q", n.Name)
			}
		case *parse.IfNode:
			err = validateReferenceBranch(n.BranchNode, trees)
		case *parse.RangeNode:
			err = validateReferenceBranch(n.BranchNode, trees)
		case *parse.WithNode:
			err = validateReferenceBranch(n.BranchNode, trees)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func validateReferenceBranch(branch parse.BranchNode, trees map[string]*parse.Tree) error {
	if err := validateReferenceList(branch.List, trees); err != nil {
		return err
	}
	return validateReferenceList(branch.ElseList, trees)
}
