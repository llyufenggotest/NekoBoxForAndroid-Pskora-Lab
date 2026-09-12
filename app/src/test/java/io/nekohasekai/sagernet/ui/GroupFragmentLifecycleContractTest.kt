package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GroupFragmentLifecycleContractTest {
    private fun source(): String = listOf(
        File("src/main/java/io/nekohasekai/sagernet/ui/GroupFragment.kt"),
        File("app/src/main/java/io/nekohasekai/sagernet/ui/GroupFragment.kt"),
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test
    fun asyncStatusBindingIsCancelledWithTheFragmentView() {
        val bindTail = source().substringAfter("groupUser.text = subscription?.username ?: \"\"")

        assertTrue(bindTail.contains("viewLifecycleOwner.lifecycleScope.launch"))
        assertTrue(bindTail.contains("withContext(Dispatchers.IO)"))
        assertFalse(bindTail.contains("runOnDefaultDispatcher"))
        assertFalse(bindTail.contains("groupStatus.text = getString"))
        assertFalse(bindTail.contains("groupStatus.text = if (size == 0L) {\n                            getString"))
    }
}
