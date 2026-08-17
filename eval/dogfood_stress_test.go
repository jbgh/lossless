package eval

// Dogfood stress: synthetic Grok sessions shaped like the failures we
// hit while using lossless on lossless, memora, and the other local
// repos. Each case is a new way to poison extract or flood ask.

import (
	"fmt"
	"testing"
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestDogfoodStress(t *testing.T) {
	t.Run("metaCommentaryDoesNotDrownGold", testMetaCommentary)
	t.Run("markdownChromeStaysOut", testMarkdownChrome)
	t.Run("agentPromptsAreNotConstraints", testAgentPrompts)
	t.Run("planningIsNotADecision", testPlanning)
	t.Run("ciStatusFailedsStayOut", testCIStatus)
	t.Run("gitURLDoesNotGroundAFailed", testGitURLPath)
	t.Run("strippedAbsAndKeyPathsDropped", testStrippedAbs)
	t.Run("failedFloodRespectsTypeCap", testFailedFlood)
	t.Run("fixtureDroppedWhenLiveWorkExists", testFixtureBleed)
	t.Run("compactShrinkThenNewGold", testCompactOscillate)
	t.Run("crossProjectNoBleed", testCrossProject)
	t.Run("secretLineNotAClaim", testSecret)
	t.Run("offTopicFailedLosesToOnPath", testOffTopicFailed)
	t.Run("warningsCiteOnlyPackedIDs", testWarningsBound)
	t.Run("concurrentPoisonedCatchUpAndAsk", testConcurrentPoison)
	t.Run("backtickFailedOnly", testBacktickFailed)
	t.Run("truncatedFragmentsDropped", testTruncated)
	t.Run("failedItemsIsNotAFailure", testFailedAsObject)
	t.Run("askPacketDoesNotEcho", testAskPacketEcho)
	t.Run("constraintFloodKeepsPathful", testConstraintFlood)
	t.Run("stateFloodKeepsDecision", testStateFlood)
	t.Run("sameSessionConcurrentCatchUp", testSameSessionConcurrent)
	t.Run("threwAbortWithoutFileIsStatus", testThrewAbortStatus)
	t.Run("failureInFilenameIsNotFailed", testFailureInFilename)
	t.Run("windowsAndUNCPathsDropped", testWindowsUNC)
	t.Run("yearOldGoldStillPacks", testYearOldGold)
	t.Run("partialLineThenComplete", testPartialLine)
	t.Run("sessionIDCannotEscapeStore", testSessionIDEscape)
	t.Run("assistantQuoteIsNotConstraint", testAssistantQuoteConstraint)
	t.Run("askTraversalPathsDoNotStatOut", testAskTraversalPaths)
	t.Run("claudePartsStillExtract", testClaudeParts)
	// Wave 3: leftover holes after waves 1–2 (MCP-prefixed tools,
	// fenced/prefixed own packets, revert-as-failed, .git grounding,
	// isError stomping decisions, numbered-list chrome).
	t.Run("prefixedMCPToolNameDoesNotEcho", testPrefixedMCPTool)
	t.Run("fencedAskPacketDoesNotEcho", testFencedAskPacket)
	t.Run("prefixedAskJSONDoesNotEcho", testPrefixedAskJSON)
	t.Run("decidedToRevertIsDecision", testDecidedToRevert)
	t.Run("dotGitPathDoesNotGroundFailed", testDotGitPath)
	t.Run("errorFlagDoesNotStompDecision", testErrorFlagDecision)
	t.Run("numberedListChromeStaysOut", testNumberedList)
	t.Run("neverMindIsNotConstraint", testNeverMind)
	t.Run("hugeMessageStillExtractsTail", testHugeMessageTail)
	t.Run("planningGoWithIsNotDecision", testPlanningGoWith)
	t.Run("dontPushYetIsSessionOp", testDontPushYet)
	t.Run("exceptionToTheRuleIsNotFailed", testExceptionTo)
	t.Run("githubActionsJobIsStatus", testGitHubActionsStatus)
	t.Run("systemReminderMidMessageDropped", testSystemReminderMid)
	t.Run("percentEncodedTraversalDropped", testPercentEncodedPath)
	// Wave 4: leftover holes after wave-3 (quoted old decisions,
	// node_modules grounding, remember-prose echo, hyphenated MCP names).
	t.Run("assistantQuoteIsNotDecision", testAssistantQuoteDecision)
	t.Run("nodeModulesDoesNotGroundFailed", testNodeModulesPath)
	t.Run("rememberProseDoesNotEcho", testRememberProse)
	t.Run("hyphenatedMCPToolDoesNotEcho", testHyphenatedMCPTool)
	t.Run("dontWaitIsSessionOp", testDontWait)
	t.Run("piTypeMessageStillExtracts", testPiTypeMessage)
	t.Run("claimJSONArrayDoesNotEcho", testClaimJSONArray)
	// Wave 5: paste dumps, planning-as-decision leftovers, build dirs.
	t.Run("pastedGoTestOutputIsStatus", testPastedGoTest)
	t.Run("illImplementIsPlanning", testIllImplement)
	t.Run("distPathDoesNotGroundFailed", testDistPath)
	t.Run("bomJSONLStillExtracts", testBOMJSONL)
	// Wave 6: same-message path bleed, harness shape leftovers.
	t.Run("sameMessageConstraintDoesNotInheritPath", testSameMessagePathBleed)
	t.Run("sameMessageDecoyFailedDoesNotInheritPath", testSameMessageFailedBleed)
	t.Run("contentObjectTextStillExtracts", testContentObjectText)
	t.Run("titleCaseHumanRoleIsUser", testHumanRole)
	t.Run("doesntWorkIsFailed", testDoesntWork)
	t.Run("windowsSepPathInText", testWindowsSepPath)
	t.Run("dotSlashPathNormalized", testDotSlashPath)
	t.Run("agentReminderMidMessageDropped", testAgentReminder)
	t.Run("jestFailLineIsStatus", testJestFail)
	t.Run("illRewriteIsPlanning", testIllRewrite)
	t.Run("weDontHaveTimeIsSessionOp", testWeDontHaveTime)
	t.Run("crlfJSONLStillExtracts", testCRLFJSONL)
	t.Run("envrcAndTargetPathsDropped", testEnvrcTarget)
	// Wave 7: keep going until a wave is clean.
	t.Run("systemRoleDoesNotExtract", testSystemRole)
	t.Run("htmlCommentIsNotDecision", testHTMLComment)
	t.Run("contentPartsObjectStillExtracts", testContentParts)
	t.Run("nestedBashToolResultNotExtracted", testNestedBashTool)
	t.Run("illMigrateIsPlanning", testIllMigrate)
	t.Run("claudeUserContextDropped", testClaudeUserContext)
	t.Run("credentialsJSONDropped", testCredentialsJSON)
	t.Run("choseWrongIsNotDecision", testChoseWrong)
	t.Run("leadingWSJSONLStillExtracts", testLeadingWS)
	t.Run("mustBeABugIsNotConstraint", testMustBeABug)
	// Wave 8: stop only when a new wave is clean.
	t.Run("developerRoleDoesNotExtract", testDeveloperRole)
	t.Run("pluginPrefixedAskDoesNotEcho", testPluginPrefixedAsk)
	t.Run("almostPickedIsNotDecision", testAlmostPicked)
	t.Run("outputTextPartsStillExtract", testOutputTextParts)
	t.Run("tabPrefixedJSONLStillExtracts", testTabPrefixedJSONL)
	t.Run("dotAndEmptyPathsDropped", testDotPaths)
	t.Run("customMessageTypeSkipped", testCustomMessage)
	t.Run("yamlAskPacketDoesNotEcho", testYAMLAskPacket)
	t.Run("userDecisionStillExtracts", testUserDecisionKept)
	// Wave 9: prove the suite is closed.
	t.Run("tomlAskPacketDoesNotEcho", testTOMLAskPacket)
	t.Run("chatMLWrapperStillExtracts", testChatML)
	t.Run("gitConflictIsNotDecision", testGitConflict)
	t.Run("upperUSERRoleStillUser", testUpperUSER)
	t.Run("nullByteLineThenGold", testNullByteLine)
	t.Run("iniAskPacketDoesNotEcho", testINIAskPacket)
	t.Run("weWillUseNextHourIsPlanning", testWeWillUseHour)
	t.Run("diffHunkIsNotFailed", testDiffHunk)
	// Wave 10: proving wave — must be clean or we keep going.
	t.Run("curlyDontWaitIsSessionOp", testCurlyDontWait)
	t.Run("modelRoleUpperStillExtracts", testModelRoleUpper)
	t.Run("venvPathDropped", testVenvPath)
	t.Run("prettyAskJSONDoesNotEcho", testPrettyAskJSON)
	t.Run("redactedLineThenGold", testRedactedThenGold)
	t.Run("illRefactorIsPlanning", testIllRefactor)
	t.Run("emptyObjectSkipped", testEmptyObject)
	t.Run("nonJSONLineThenGold", testNonJSONLine)
}

func dogfoodStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func dogfoodCatch(t *testing.T, st *store.Store, project, sid, body string) write.CatchUpResult {
	t.Helper()
	p := testdataWrite(t, t.TempDir(), sid+".jsonl", body)
	res, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: project, Harness: "grok", SessionID: sid, Source: "turn",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func grokLine(role, text string) string {
	return fmt.Sprintf(`{"type":%q,"content":%q}`+"\n", role, text)
}

func askAtNow(t *testing.T, st *store.Store, req retrieve.Request) retrieve.Response {
	t.Helper()
	eng := retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	}}
	out, err := eng.Ask(req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func contextHas(out retrieve.Response, needle string) bool {
	return containsText(out, needle)
}

func contextHasAny(out retrieve.Response, needles ...string) bool {
	for _, n := range needles {
		if containsText(out, n) {
			return true
		}
	}
	return false
}

func countType(out retrieve.Response, typ string) int {
	n := 0
	for _, h := range out.Context {
		if h.Type == typ {
			n++
		}
	}
	return n
}
