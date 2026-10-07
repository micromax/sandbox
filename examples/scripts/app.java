import java.util.List;
import java.util.stream.Collectors;

public class Main {
    public static void main(String[] args) {
        List<String> langs = List.of("TypeScript", "JavaScript", "Python", "Lua", "Java", "Rust", "Go");

        List<String> upper = langs.stream()
                .map(String::toUpperCase)
                .collect(Collectors.toList());

        System.out.println("=== Java 21 Language Pack ===");
        System.out.println("Java Vendor  : " + System.getProperty("java.vendor"));
        System.out.println("Java Version : " + System.getProperty("java.version"));
        System.out.println("Languages    : " + upper);
        System.out.println("Status: OK");
    }
}
