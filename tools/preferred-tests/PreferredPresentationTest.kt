package io.nekohasekai.sagernet.ui

fun main() {
    check(preferredMemberLatency(0, 43) == "未测试") // stale ping is not proof of success
    check(preferredMemberLatency(1, 43) == "43 ms")
    check(preferredMemberLatency(1, 0) == "0 ms")
    check(preferredMemberLatency(1, -1) == "未测试")
    check(preferredMemberLatency(2, 43) == "测试失败")
    check(preferredMemberLatency(3, 43) == "测试失败")
    check(preferredMemberLatency(99, 43) == "未测试")
    println("PASS: 7 persisted-source latency presentation cases")
}
