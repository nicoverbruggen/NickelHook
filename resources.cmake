# The generator runs on the build host even when the mod targets ARM.
set(NH_RESOURCE_GENERATOR "${CMAKE_CURRENT_LIST_DIR}/embed_resources.c")
function(nh_embed_resources target)
    find_program(NH_HOST_CC NAMES cc gcc clang NO_CMAKE_FIND_ROOT_PATH REQUIRED)
    set(tool "${CMAKE_CURRENT_BINARY_DIR}/embed_resources-${CMAKE_HOST_SYSTEM_NAME}-${CMAKE_HOST_SYSTEM_PROCESSOR}")
    add_custom_command(OUTPUT "${tool}"
        COMMAND "${NH_HOST_CC}" -std=c99 -Wall -Wextra -Werror -O2 "${NH_RESOURCE_GENERATOR}" -o "${tool}"
        COMMAND chmod 755 "${tool}"
        DEPENDS "${NH_RESOURCE_GENERATOR}" VERBATIM)
    set(header "${CMAKE_CURRENT_BINARY_DIR}/embedded_resources.h")
    add_custom_command(OUTPUT "${header}"
        COMMAND "${tool}" "${header}" ${ARGN}
        DEPENDS "${tool}" ${ARGN} VERBATIM)
    target_sources(${target} PRIVATE "${header}")
    target_include_directories(${target} PRIVATE "${CMAKE_CURRENT_BINARY_DIR}")
endfunction()
