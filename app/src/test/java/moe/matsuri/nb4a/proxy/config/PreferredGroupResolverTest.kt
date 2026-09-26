package moe.matsuri.nb4a.proxy.config

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.AbstractBean
import io.nekohasekai.sagernet.fmt.KryoConverters
import io.nekohasekai.sagernet.fmt.internal.ChainBean
import io.nekohasekai.sagernet.fmt.socks.SOCKSBean
import io.nekohasekai.sagernet.fmt.trojan.TrojanBean
import io.nekohasekai.sagernet.fmt.trojan_go.TrojanGoBean
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import org.junit.Assert.*
import org.junit.Test

class PreferredGroupResolverTest {
    private fun entity(id: Long, group: Long = 2, bean: AbstractBean = SOCKSBean()) =
        ProxyEntity().apply {
            this.id = id; groupId = group; userOrder = id
            bean.initializeDefaultValues(); bean.name = "node-$id"; putBean(bean)
        }
    private fun preferred(id: Long = 10, members: List<Long> = emptyList(), sources: List<Long> = emptyList()) =
        entity(id, 1, ConfigBean().apply { type = 2; preferredMemberIds = members; preferredSourceGroupIds = sources })
    private fun chain(id: Long, vararg members: Long) = entity(id, bean = ChainBean().apply { proxies = members.toList() })
    private fun groups() = listOf(ProxyGroup(id = 1, name = "preferred"), ProxyGroup(id = 2, name = "subscription"))
    private fun resolver(vararg profiles: ProxyEntity) = PreferredGroupResolver(profiles.toList(), groups())
    private fun rejects(block: () -> Unit) = assertThrows(IllegalArgumentException::class.java, block)

    @Test fun newSnapshotReflectsSubscriptionReplacementInUserOrder() {
        val owner = preferred(sources = listOf(2))
        val old = entity(1)
        val first = resolver(owner, old)
        assertEquals(listOf(1L), first.members(owner).map { it.id })
        val a = entity(2).apply { userOrder = 20 }
        val b = entity(3).apply { userOrder = 10 }
        val updated = resolver(owner, a, b)
        updated.validate(owner)
        assertEquals(listOf(3L, 2L), updated.members(owner).map { it.id })
        assertEquals(listOf(1L), first.members(owner).map { it.id })
    }

    @Test fun explicitAndDynamicOverlapIsDeduplicatedWithExplicitOrderFirst() {
        val owner = preferred(members = listOf(2), sources = listOf(2))
        val r = resolver(owner, entity(1), entity(2))
        r.validate(owner)
        assertEquals(listOf(2L, 1L), r.members(owner).map { it.id })
    }

    @Test fun deletionOfExplicitNodeSourceGroupOrLastSourceMemberFails() {
        val explicit = preferred(members = listOf(1))
        rejects { resolver(explicit).validate(explicit) }
        val dynamic = preferred(sources = listOf(2))
        rejects { resolver(dynamic).validate(dynamic) }
        rejects { PreferredGroupResolver(listOf(dynamic, entity(1)), groups().take(1)).validate(dynamic) }
    }

    @Test fun pruneDropsDeletedMemberSourceAndExcludedReferencesButKeepsValidOnes() {
        val spec = PreferredGroupSpec(
            memberIds = listOf(1, 2, 3),
            sourceGroupIds = listOf(2, 9),
            intervalSeconds = 120,
            excludedMemberIds = listOf(3, 8),
        )
        val pruned = spec.pruned(existingIds = setOf(1L, 3L), existingGroupIds = setOf(2L))
        assertEquals(listOf(1L, 3L), pruned.memberIds)
        assertEquals(listOf(2L), pruned.sourceGroupIds)
        assertEquals(listOf(3L), pruned.excludedMemberIds)
        assertEquals(120, pruned.intervalSeconds)
        assertEquals(pruned, pruned.pruned(setOf(1L, 3L), setOf(2L)))
    }

    @Test fun prunedContainerValidatesAfterExplicitMemberDeletionWhenValidRemains() {
        val owner = preferred(members = listOf(1, 2))
        rejects { resolver(owner, entity(1)).validate(owner) }
        val pruned = owner.configBean!!.preferredSpec().pruned(setOf(1L), setOf(1L, 2L))
        owner.configBean!!.preferredMemberIds = pruned.memberIds
        owner.putBean(owner.configBean!!)
        resolver(owner, entity(1)).validate(owner)
        assertEquals(listOf(1L), resolver(owner, entity(1)).members(owner).map { it.id })
    }

    @Test fun nestedPreferredIsExplicitlyRejectedWithoutDroppingMembers() {
        val root = preferred(members = listOf(11, 12))
        val left = preferred(11, listOf(1)); val right = preferred(12, listOf(1)); val leaf = entity(1)
        val r = resolver(root, left, right, leaf)
        assertTrue(rejects { r.validate(root) }.message!!.contains("不支持嵌套优选"))
        assertEquals(listOf(11L, 12L), r.members(root).map { it.id })
        r.validate(left)
        r.validate(right)
        left.configBean!!.preferredMemberIds = listOf(10)
        rejects { resolver(root, left, right, leaf).validate(root) }
        val self = preferred(members = listOf(10))
        rejects { resolver(self).validate(self) }
    }

    @Test fun pickerRejectsPreferredEvenWhenStandaloneValid() {
        val root = preferred(members = listOf(1)); val leaf = entity(1)
        val r = resolver(root, leaf)
        assertNull(r.unsupportedReason(root))
        assertTrue(r.candidateUnsupportedReason(root)!!.contains("不支持嵌套"))
        assertNull(r.candidateUnsupportedReason(leaf))
    }

    @Test fun savingIntoDynamicallyReferencedGroupRejectsBeforeMutation() {
        val existing = preferred(sources = listOf(2)); val leaf = entity(1)
        val candidate = preferred(0, listOf(1)).apply { groupId = 2 }
        val error = rejects { validatePreferredSave(candidate, listOf(existing, leaf), groups()) }
        assertTrue(error.message!!.contains("不支持嵌套"))
        assertEquals(0L, candidate.id)
        resolver(existing, leaf).validate(existing)
        val safe = preferred(0, listOf(1))
        validatePreferredSave(safe, listOf(existing, leaf), groups())
        validatePreferredSave(preferred(members = listOf(1)), listOf(existing, leaf), groups())
    }

    @Test fun dynamicSourceCycleIsRejected() {
        val root = preferred(sources = listOf(2))
        val nested = preferred(11, listOf(10)).apply { groupId = 2 }
        rejects { resolver(root, nested).validate(root) }
    }

    @Test fun standardSocksTrojanAndOrdinaryVlessAreAccepted() {
        val root = preferred(members = listOf(1, 2, 3))
        val vless = VMessBean().apply { alterId = -1; uuid = "123e4567-e89b-82d3-a456-426614174000" }
        assertNull(resolver(root, entity(1), entity(2, bean = TrojanBean()), entity(3, bean = vless)).unsupportedReason(root))
    }

    @Test fun tunNetExternalAndCustomOutboundCandidatesFailClosed() {
        val root = preferred(members = listOf(1))
        val unsupported = listOf<AbstractBean>(
            VMessBean().apply { alterId = -1; uuid = "123e4567-e89b-82d3-a456-426614174000#TunNet" },
            TrojanGoBean(), SOCKSBean().apply { customOutboundJson = "{}" },
            ConfigBean().apply { type = 1; config = "{\"type\":\"direct\"}" })
        unsupported.forEach { bean -> assertNotNull(resolver(root, entity(1, bean = bean)).unsupportedReason(root)) }
    }

    @Test fun chainsAcceptStandardHopsAndRejectMissingEmptyCyclicOrPreferredHops() {
        val root = preferred(members = listOf(4)); val leaf = entity(1)
        resolver(root, chain(4, 1), leaf).validate(root)
        for (bad in listOf(chain(4), chain(4, 9), chain(4, 4), chain(4, 10)))
            rejects { resolver(root, bad, leaf).validate(root) }
    }

    @Test fun detourReferencesAreValidatedIncludingCycleAndForbiddenChainHop() {
        val root = preferred(members = listOf(1)); val leaf = entity(1); val hop = entity(2)
        val validGroups = groups().map { if (it.id == 2L) it.copy(frontProxy = 2) else it }
        // Both candidates in the same group would make hop 2 self-reference when directly selected;
        // in a detour, group detours are intentionally not applied again.
        PreferredGroupResolver(listOf(root, leaf, hop), validGroups).validate(root)
        rejects { PreferredGroupResolver(listOf(root, leaf), validGroups).validate(root) }
        rejects { PreferredGroupResolver(listOf(root, leaf, chain(2, 1)), validGroups).validate(root) }
        val selfGroups = groups().map { if (it.id == 2L) it.copy(frontProxy = 1) else it }
        rejects { PreferredGroupResolver(listOf(root, leaf), selfGroups).validate(root) }
        val ownerDetour = groups().map { if (it.id == 1L) it.copy(landingProxy = 1) else it }
        rejects { PreferredGroupResolver(listOf(root, leaf), ownerDetour).validate(root) }
    }

    @Test fun restoreValidatorRejectsDuplicateIdsAndDanglingPreferredReferences() {
        val root = preferred(members = listOf(1)); val leaf = entity(1)
        validatePreferredRestoreSnapshot(listOf(root, leaf), groups())
        for (invalid in listOf(listOf(root, leaf, entity(1)), listOf(root, entity(0)), listOf(root)))
            rejects { validatePreferredRestoreSnapshot(invalid, groups()) }
        rejects { validatePreferredRestoreSnapshot(listOf(root, leaf), groups() + groups().first()) }
        rejects { validatePreferredRestoreSnapshot(listOf(root, leaf), listOf(ProxyGroup(id = 0))) }
        val brokenOwner = preferred(11, listOf(999))
        rejects { validatePreferredRestoreSnapshot(listOf(root, leaf, brokenOwner), groups()) }
    }

    @Test fun restoreRejectsPreferredOwnerWithDeletedContainerGroup() {
        val root = preferred(members = listOf(1))
        rejects { validatePreferredRestoreSnapshot(listOf(root, entity(1)), groups().filter { it.id != 1L }) }
    }

    @Test fun restoredDetourReferencesRemainValidated() {
        val root = preferred(members = listOf(1)); val leaf = entity(1)
        val incoming = groups().map { if (it.id == 2L) it.copy(landingProxy = 99) else it }
            .map { KryoConverters.deserialize(ProxyGroup(), KryoConverters.serialize(it)) }
        rejects { validatePreferredRestoreSnapshot(listOf(root, leaf), incoming) }
        validatePreferredRestoreSnapshot(listOf(root, leaf, entity(99)), incoming)
    }

    @Test fun restoredBeansKeepIdsAndResolveAgainstRestoredGroupSnapshot() {
        val root = preferred(members = listOf(4), sources = listOf(2))
        val profiles = listOf(root, entity(1), chain(4, 1))
        val restored = profiles.map { original ->
            KryoConverters.deserialize(ProxyEntity(), KryoConverters.serialize(original))
        }
        val restoredGroups = groups().map { KryoConverters.deserialize(ProxyGroup(), KryoConverters.serialize(it)) }
        val r = PreferredGroupResolver(restored, restoredGroups)
        r.validate(restored.first())
        assertEquals(listOf(4L, 1L), r.members(restored.first()).map { it.id })
        rejects { PreferredGroupResolver(restored.filter { it.id != 1L }, restoredGroups).validate(restored.first()) }
    }
}
