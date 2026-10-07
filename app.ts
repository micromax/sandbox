interface User {
    id: number;
    name: string;
    role: "admin" | "developer";
}

enum Status {
    Active = 1,
    Pending = 2,
}

function display(user: User, status: Status): string {
    return `[Status: ${status}] User #${user.id} (${user.name}) - Role: ${user.role}`;
}

const u: User = {
    id: 101,
    name: "Antigravity",
    role: "developer",
};

console.log(display(u, Status.Active));
console.log("TypeScript running in WebAssembly sandbox successfully!");
