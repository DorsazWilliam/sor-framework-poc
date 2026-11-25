package main

import (
	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/modules/accounting"
	"github.com/DorsazWilliam/sor-framework-poc/modules/base"
	"github.com/DorsazWilliam/sor-framework-poc/modules/billing"
	"github.com/DorsazWilliam/sor-framework-poc/modules/time"
)

// modules are the domain modules in schema order; the kernel's Core comes before them.
var modules = []kernel.Module{base.Module, time.Module, billing.Module, accounting.Module}
