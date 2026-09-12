import moe.matsuri.nb4a.proxy.config.*

fun main() {
    var count = 0
    fun test(name: String, block: () -> Unit) { block(); count++; println("PASS $name") }
    fun fails(block: () -> Unit) { check(runCatching(block).exceptionOrNull() is IllegalArgumentException) }
    test("explicit members") { check(PreferredGroupSpec(listOf(1)).validate(setOf(1), emptySet()) == listOf(1L)) }
    test("dynamic source without explicit members") { PreferredGroupSpec(sourceGroupIds=listOf(2)).validate(emptySet(),setOf(2),3) }
    test("missing member") { fails { PreferredGroupSpec(listOf(9)).validate(setOf(1), emptySet()) } }
    test("missing source") { fails { PreferredGroupSpec(sourceGroupIds=listOf(9)).validate(emptySet(),setOf(2)) } }
    test("owner source self reference") { fails { PreferredGroupSpec(sourceGroupIds=listOf(2)).validate(emptySet(),setOf(2),2) } }
    test("duplicates rejected") { fails { PreferredGroupSpec(listOf(1,1)).validate(setOf(1),emptySet()) } }
    test("empty rejected") { fails { PreferredGroupSpec().validate(emptySet(),emptySet()) } }
    test("interval bounds") { fails { PreferredGroupSpec(listOf(1), intervalSeconds=0).validate(setOf(1),emptySet()) } }
    test("tolerance bounds") { fails { PreferredGroupSpec(listOf(1), minDelayMilliseconds=-1).validate(setOf(1),emptySet()) } }
    test("zero tolerance explicit not core default") { check(nativeUrlTest("p",listOf("a"),300,0)["tolerance"] == 0) }
    test("native fields") { val o=nativeUrlTest("p",listOf("a","b"),60,50); check(o["interval"]=="60s" && o["type"]=="urltest" && o["tolerance"]==50) }
    test("native self tag") { fails { nativeUrlTest("p",listOf("p"),60,0) } }
    test("cycle path") { val g=PreferredReferenceGuard(); fails { g.visit(1) { g.visit(2) { g.visit(1) {} } } } }
    test("shared descendant valid") { val g=PreferredReferenceGuard(); g.visit(1) { g.visit(2) {}; g.visit(2) {} } }
    test("guard cleanup after error") { val g=PreferredReferenceGuard(); fails { g.visit(1) { g.visit(1) {} } }; g.visit(1) {} }
    println("$count model tests passed")
}
