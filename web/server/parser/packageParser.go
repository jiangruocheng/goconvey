package parser

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/smartystreets/goconvey/web/server/contract"
)

var (
	testNamePattern = regexp.MustCompile("^=== RUN:? (.+)$")
)

func ParsePackageResults(result *contract.PackageResult, rawOutput string) {
	newOutputParser(result, rawOutput).parse()
}

type outputParser struct {
	raw    string
	lines  []string
	result *contract.PackageResult
	tests  []*contract.TestResult

	packageLines []string // Output generated outside the call stack of test functions (like stuff from init()).

	// place holders for loops
	line    string
	test    *contract.TestResult
	testMap map[string]*contract.TestResult
}

func newOutputParser(result *contract.PackageResult, rawOutput string) *outputParser {
	self := new(outputParser)
	self.raw = strings.TrimSpace(rawOutput)
	self.lines = strings.Split(self.raw, "\n")
	self.result = result
	self.tests = []*contract.TestResult{}
	self.testMap = make(map[string]*contract.TestResult)
	return self
}

func (self *outputParser) parse() {
	self.separateTestFunctionsAndMetadata()
	self.parseEachTestFunction()
	self.recoverFromInitPanic()
}

func (self *outputParser) separateTestFunctionsAndMetadata() {
	for _, self.line = range self.lines {
		if self.processNonTestOutput() {
			break
		}
		self.processTestOutput()
	}
}
func (self *outputParser) processNonTestOutput() bool {
	if noGoFiles(self.line) {
		self.recordFinalOutcome(contract.NoGoFiles)

	} else if buildFailed(self.line) {
		self.recordFinalOutcome(contract.BuildFailure)

	} else if noTestFiles(self.line) {
		self.recordFinalOutcome(contract.NoTestFiles)

	} else if noTestFunctions(self.line) {
		self.recordFinalOutcome(contract.NoTestFunctions)

	} else {
		return false
	}
	return true
}

func (self *outputParser) recordFinalOutcome(outcome string) {
	self.result.Outcome = outcome
	self.result.BuildOutput = strings.Join(self.lines, "\n")
}

func (self *outputParser) processTestOutput() {
	if isNewTest(self.line) {
		self.registerTestFunction()

	} else if isTestResult(self.line) {
		self.recordTestMetadata()

	} else if isPackageReport(self.line) {
		self.recordPackageMetadata()

	} else {
		self.saveLineForParsingLater()

	}
}

func (self *outputParser) registerTestFunction() {
	testName := testNamePattern.FindStringSubmatch(self.line)[1]
	self.test = contract.NewTestResult(testName)
	self.tests = append(self.tests, self.test)
	self.testMap[self.test.TestName] = self.test
}
func (self *outputParser) recordTestMetadata() {
	testName := strings.Split(self.line, " ")[2]
	self.test = self.testMap[testName]
	self.test.Passed = !strings.HasPrefix(self.line, "--- FAIL: ")
	self.test.Skipped = strings.HasPrefix(self.line, "--- SKIP: ")
	self.test.Elapsed = parseTestFunctionDuration(self.line)
}
func (self *outputParser) recordPackageMetadata() {
	if packageFailed(self.line) {
		self.recordTestingOutcome(contract.Failed)

	} else if packagePassed(self.line) {
		self.recordTestingOutcome(contract.Passed)

	} else if isCoverageSummary(self.line) {
		self.recordCoverageSummary(self.line)
	}
}
func (self *outputParser) recordTestingOutcome(outcome string) {
	self.result.Outcome = outcome
	fields := strings.Split(self.line, "\t")
	self.result.PackageName = strings.TrimSpace(fields[1])
	self.result.Elapsed = parseDurationInSeconds(fields[2], 3)
}
func (self *outputParser) recordCoverageSummary(summary string) {
	start := len("coverage: ")
	end := strings.Index(summary, "%")
	value := summary[start:end]
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		self.result.Coverage = -1
	} else {
		self.result.Coverage = parsed
	}
}
func (self *outputParser) saveLineForParsingLater() {
	self.line = strings.TrimLeft(self.line, "\t")
	if self.test == nil {
		self.packageLines = append(self.packageLines, self.line)
	} else {
		self.test.RawLines = append(self.test.RawLines, self.line)
	}
}

func (self *outputParser) parseEachTestFunction() {
	for _, self.test = range self.tests {
		self.test = parseTestOutput(self.test)
		if self.test.Error != "" {
			self.result.Outcome = contract.Panicked
		}
		self.test.RawLines = []string{}
		self.result.TestResults = append(self.result.TestResults, *self.test)
	}
}

func (self *outputParser) recoverFromInitPanic() {
	panicked := false
	if len(self.tests) == 0 && len(self.packageLines) > 0 {
		for _, line := range self.packageLines {
			if strings.HasPrefix(line, "panic: ") {
				panicked = true
				break
			}
		}
	}
	if panicked {
		self.result.Outcome = contract.Panicked
		self.result.Error = strings.Join(self.packageLines, "\n")
	}
}
